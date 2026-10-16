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
	for {
		cursor, err := db.Cursor(host)
		if err != nil {
			return result, err
		}
		page, err := fetch(client, cursor.Next)
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

// fetch runs one export after since and returns the checked page.
func fetch(client Runner, since uint64) (store.Page, error) {
	command, err := kanshi.ExportCommand(since, PageSize, true)
	if err != nil {
		return store.Page{}, err
	}
	page := store.Page{Since: since}
	res, err := client.Exec(command, pageTimeout, func(r io.Reader) error {
		var readErr error
		page.Header, page.End, readErr = kanshi.ReadExport(r, since, PageSize, func(s kanshi.Sample, line []byte) error {
			page.Samples = append(page.Samples, store.PulledSample{Sample: s, Line: append([]byte(nil), line...)})
			return nil
		})
		return readErr
	})
	if err != nil {
		return store.Page{}, fmt.Errorf("kanshi export: %w", err)
	}
	if err := kanshi.Explain(Outcome(res)); err != nil {
		return store.Page{}, err
	}
	return page, nil
}
