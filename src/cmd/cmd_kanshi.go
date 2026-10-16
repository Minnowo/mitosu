// `mitosu kanshi version` and `mitosu kanshi export`: run a kanshi request
// on a server over SSH and check the response against the protocol
// (../kanshi/docs/protocol.md). Export writes the sample lines to stdout
// only after the whole response passed the checks.

package cmd

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/urfave/cli/v3"

	"mitosu/src/kanshi"
	"mitosu/src/ssh"
)

const (
	// versionTimeout bounds `kanshi version`.
	versionTimeout = 30 * time.Second
	// exportTimeout bounds one `kanshi export`.
	exportTimeout = 10 * time.Minute
	// maxExportBytes caps the samples one export call holds in memory
	// before printing them.
	maxExportBytes = 256 << 20
)

// CmdKanshiVersion prints the kanshi and protocol versions of the server.
func CmdKanshiVersion(ctx context.Context, c *cli.Command) error {
	client, err := connect(c, false)
	if err != nil {
		return err
	}
	defer client.Close()

	var v kanshi.Version
	res, err := client.Exec(kanshi.VersionCommand(), versionTimeout, func(r io.Reader) error {
		var readErr error
		v, readErr = kanshi.ReadVersion(r)
		return readErr
	})
	if err != nil {
		return err
	}
	if err := kanshiResultError(res); err != nil {
		return err
	}
	fmt.Printf("kanshi %s, protocol %s\n", kanshi.Printable(v.Kanshi), kanshi.Printable(v.Format))
	return nil
}

// CmdKanshiExport runs `kanshi export --since --limit --gzip`, checks the
// whole response, and then writes each sample line to stdout unchanged
// (JSON lines); nothing is written if any check fails. The header and end
// values go to stderr in a summary.
func CmdKanshiExport(ctx context.Context, c *cli.Command) error {
	since := c.Uint64("since")
	limit := c.Uint64("limit")
	command, err := kanshi.ExportCommand(since, limit, true)
	if err != nil {
		return err
	}

	client, err := connect(c, false)
	if err != nil {
		return err
	}
	defer client.Close()

	var samples bytes.Buffer
	count := 0
	var header kanshi.Header
	var end kanshi.End
	res, err := client.Exec(command, exportTimeout, func(r io.Reader) error {
		var readErr error
		header, end, readErr = kanshi.ReadExport(r, since, limit, func(_ kanshi.Sample, line []byte) error {
			if samples.Len()+len(line)+1 > maxExportBytes {
				return fmt.Errorf("response is over %d MiB; use a smaller --limit", maxExportBytes>>20)
			}
			count++
			samples.Write(line)
			return samples.WriteByte('\n')
		})
		return readErr
	})
	if err != nil {
		return err
	}
	if err := kanshiResultError(res); err != nil {
		return err
	}

	out := bufio.NewWriter(os.Stdout)
	if _, err := samples.WriteTo(out); err != nil {
		return err
	}
	if err := out.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "store %s (kanshi %s, protocol %s): %d samples after %d, next %d, more %v, lost through %d\n",
		header.Store, kanshi.Printable(header.Kanshi), kanshi.Printable(header.Format), count, since, end.Next, end.More, end.LostThrough)
	return nil
}

// kanshiResultError explains a failed kanshi request from the reader's
// error and what the server said. kanshi output that is not kanshi's
// first line is reported as shell noise whatever the exit status: the
// reader stops at once and Exec then cuts kanshi off, so a non-zero status
// is expected there.
func kanshiResultError(res ssh.ExecResult) error {
	if res.ReadErr == nil {
		return nil
	}
	exit := fmt.Sprintf("exit %d", res.Status)
	if res.Signal != "" {
		exit = "killed by " + kanshi.Printable(res.Signal)
	}
	if res.Stderr != "" {
		exit += ": " + kanshi.Printable(res.Stderr)
	}
	var serverErr *kanshi.ServerError
	switch {
	case res.TimedOut:
		return fmt.Errorf("kanshi did not finish in time (%s): %w", exit, res.ReadErr)
	case errors.As(res.ReadErr, &serverErr):
		return res.ReadErr
	case errors.Is(res.ReadErr, kanshi.ErrNoOutput) && res.Status == 127:
		return fmt.Errorf("kanshi is not installed on the server, or not on the login's PATH (%s)", exit)
	case errors.Is(res.ReadErr, kanshi.ErrNoOutput):
		return fmt.Errorf("kanshi printed nothing (%s)", exit)
	case errors.Is(res.ReadErr, kanshi.ErrNotKanshiOutput):
		return fmt.Errorf("%w: the login printed text before kanshi did; shell startup files (such as ~/.bashrc) must print to stderr or nothing (%s)", res.ReadErr, exit)
	}
	return fmt.Errorf("%w (%s)", res.ReadErr, exit)
}
