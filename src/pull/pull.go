// Pulls one host's new kanshi samples over SSH into the store, page by
// page, from the host's cursor until kanshi has nothing more, then
// applies retention.

package pull

import (
	"errors"
	"fmt"
	"io"
	"time"

	"mitosu/src/kanshi"
	"mitosu/src/ssh"
	"mitosu/src/store"
)

const (
	// PageSize is the --limit of each export. A page is held in memory
	// and stored in one transaction.
	PageSize = 2000

	// pageTimeout bounds one export request.
	pageTimeout = 10 * time.Minute
)

// maxPageBytes caps the sample lines one page may hold (a variable for
// tests). A page over it is asked for again with --limit set to the
// samples that fitted (at least one: a line is at most 1 MiB, the
// reader's cap), and that limit is kept for the rest of the pull (a
// host with large samples would otherwise waste every other request).
var maxPageBytes = 64 << 20

// pageTooBigError is returned by fetch for a page over maxPageBytes;
// Fitted samples came before the cap was reached.
type pageTooBigError struct {
	Fitted int
}

func (e *pageTooBigError) Error() string {
	return fmt.Sprintf("page over the %d MiB memory cap", maxPageBytes>>20)
}

// Runner runs a remote command; *ssh.SSHClient is one.
type Runner interface {
	Exec(command string, timeout time.Duration, read func(io.Reader) error) (ssh.ExecResult, error)
}

// Result tells what one Pull did.
type Result struct {
	// Samples is how many samples were stored.
	Samples int
	// Pages is how many export requests were made.
	Pages int
	// StoreReset is true if the server's store had been recreated.
	StoreReset bool
	// Cursor is where the next pull starts.
	Cursor store.Cursor
}

// Pull stores every sample kanshi on client has after host's cursor in
// db, with tiers (see store.ValidateTiers), then prunes db. now gives the
// current time. A page that fails any check is not stored; the error
// says why (see ExplainResult) and the next Pull retries from the same
// cursor.
func Pull(client Runner, db *store.DB, host string, tiers []store.Tier, now func() time.Time) (Result, error) {
	var result Result
	if err := store.ValidateTiers(tiers); err != nil {
		return result, err
	}
	limit := PageSize
	for {
		cursor, err := db.Cursor(host)
		if err != nil {
			return result, err
		}
		page, err := fetch(client, cursor.Next, limit)
		var tooBig *pageTooBigError
		if errors.As(err, &tooBig) && limit > 1 {
			result.Pages++
			limit = max(1, tooBig.Fitted)
			continue
		}
		if err != nil {
			return result, err
		}
		result.Pages++
		page.PulledAtMs = uint64(now().UnixMilli())

		if cursor.Store != "" && page.Header.Store != cursor.Store {
			if result.StoreReset {
				return result, errors.New("the server's kanshi store changed twice during one pull")
			}
			// The page was read with the old cursor; start the new store
			// from 0.
			if err := db.ResetStore(host, page.Header.Store, page.PulledAtMs); err != nil {
				return result, err
			}
			result.StoreReset = true
			continue
		}

		n, err := db.Ingest(host, tiers, page)
		if err != nil {
			return result, err
		}
		result.Samples += n
		if !page.End.More {
			break
		}
	}
	if _, err := db.Prune(tiers, now()); err != nil {
		return result, err
	}
	cursor, err := db.Cursor(host)
	result.Cursor = cursor
	return result, err
}

// fetch runs one export of at most limit samples after since and returns
// the checked page, or a *pageTooBigError.
func fetch(client Runner, since uint64, limit int) (store.Page, error) {
	command, err := kanshi.ExportCommand(since, uint64(limit), true)
	if err != nil {
		return store.Page{}, err
	}
	page := store.Page{Since: since}
	held, fitted := 0, 0
	res, err := client.Exec(command, pageTimeout, func(r io.Reader) error {
		var readErr error
		page.Header, page.End, readErr = kanshi.ReadExport(r, since, uint64(limit), func(s kanshi.Sample, line []byte) error {
			held += len(line)
			if held > maxPageBytes {
				return &pageTooBigError{Fitted: fitted}
			}
			fitted++
			page.Samples = append(page.Samples, store.PulledSample{Sample: s, Line: append([]byte(nil), line...)})
			return nil
		})
		return readErr
	})
	if err != nil {
		return store.Page{}, fmt.Errorf("kanshi export: %w", err)
	}
	var tooBig *pageTooBigError
	if errors.As(res.ReadErr, &tooBig) {
		return store.Page{}, tooBig
	}
	if err := kanshi.Explain(Outcome(res)); err != nil {
		return store.Page{}, err
	}
	return page, nil
}
