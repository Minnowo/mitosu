// Live views (docs/brief.md section 3, "Live views"): fixed commands run
// over the admin login when the user opens a view, never stored. This
// file runs one command with caps on time and output, and turns the
// common failures (tool missing, no permission, sudo needing a password)
// into errors that say what to do.

package live

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"mitosu/src/kanshi"
	"mitosu/src/ssh"
)

const (
	// timeout bounds one live command.
	timeout = 20 * time.Second
	// maxOutput caps the stdout read from one command; the rest is cut
	// off and the result marked truncated.
	maxOutput = 4 << 20
	// MaxLines caps the lines or entries one view asks for.
	MaxLines = 2000
)

// Runner runs a command on a host (scheduler.Scheduler.Exec).
type Runner interface {
	Exec(ctx context.Context, host, command string, timeout time.Duration, read func(io.Reader) error) (ssh.ExecResult, error)
}

// Options are the live view settings from the config.
type Options struct {
	// Sudo runs docker and journalctl through `sudo -n` (live_sudo).
	Sudo bool
}

// ErrBadInput is returned (wrapped) for a request value that is not
// allowed, before anything runs.
var ErrBadInput = errors.New("bad input")

// output is what a command printed on stdout (at most maxOutput bytes)
// and its stderr.
type output struct {
	stdout    []byte
	truncated bool
	stderr    string
	status    int
}

// run runs command on host and returns its output, or an error that says
// what went wrong in words a user can act on. tool names the program for
// those messages. Output over maxOutput is cut off at the end, or with
// keepEnd at the start (the whole output is read, and the first line
// kept is a whole one).
func run(ctx context.Context, r Runner, host, tool, command string, keepEnd bool) (output, error) {
	var out output
	res, err := r.Exec(ctx, host, command, timeout, func(rd io.Reader) error {
		if keepEnd {
			b, truncated, err := readEnd(rd, maxOutput)
			out.stdout, out.truncated = b, truncated
			return err
		}
		var buf bytes.Buffer
		n, err := io.Copy(&buf, io.LimitReader(rd, maxOutput+1))
		if err != nil {
			return err
		}
		if n > maxOutput {
			out.truncated = true
			buf.Truncate(maxOutput)
		}
		out.stdout = buf.Bytes()
		return nil
	})
	if err != nil {
		return output{}, err
	}
	out.stderr = res.Stderr
	out.status = res.Status
	if res.TimedOut {
		return output{}, fmt.Errorf("%s did not finish within %s", tool, timeout)
	}
	if res.ReadErr != nil {
		return output{}, res.ReadErr
	}
	// Only a failed command is explained, from its stderr (output on
	// stdout is the server's data: never used for messages or logs).
	if res.Status == 0 || out.truncated {
		return out, nil
	}
	lower := strings.ToLower(res.Stderr)
	switch {
	case isSudoRefusal(lower):
		return output{}, errSudoPassword(tool)
	case res.Status == 127 || strings.Contains(lower, tool+": command not found") || strings.Contains(lower, tool+": not found"):
		// (sudo reports a missing program as "sudo: docker: command not
		// found", exit 1.)
		return output{}, fmt.Errorf("%s is not installed on the server, or not on the login's PATH", tool)
	case tool == "docker" && strings.Contains(lower, "permission denied"):
		return output{}, errors.New("this login may not use Docker: add it to group docker on the server, or set live_sudo = on")
	}
	return output{}, fmt.Errorf("%s failed (exit %d): %s", tool, res.Status, kanshi.Printable(strings.TrimSpace(res.Stderr)))
}

// isSudoRefusal reports whether lower-cased text is sudo -n (or sudo-rs)
// refusing to run without a password.
func isSudoRefusal(lower string) bool {
	return strings.Contains(lower, "a password is required") || strings.Contains(lower, "a terminal is required") ||
		strings.Contains(lower, "interactive authentication is required")
}

// errSudoPassword says that live_sudo cannot work as set up.
func errSudoPassword(tool string) error {
	return fmt.Errorf("live_sudo is on, but sudo asks this login for a password: allow it without one (NOPASSWD) for %s, or set live_sudo = off", tool)
}

// sudo returns the prefix for privileged commands.
func (o Options) sudo() string {
	if o.Sudo {
		return "sudo -n "
	}
	return ""
}

// readEnd reads rd to the end and returns its last max bytes at most,
// starting after a newline if anything was dropped, and whether anything
// was. It holds about 2*max bytes at most (plus one read and the slack
// of a growing slice).
func readEnd(rd io.Reader, max int) ([]byte, bool, error) {
	buf := make([]byte, 0, 64*1024)
	chunk := make([]byte, 64*1024)
	dropped := false
	for {
		n, err := rd.Read(chunk)
		buf = append(buf, chunk[:n]...)
		if len(buf) > 2*max {
			buf = append(buf[:0], buf[len(buf)-max:]...)
			dropped = true
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, false, err
		}
	}
	if len(buf) > max {
		buf = buf[len(buf)-max:]
		dropped = true
	}
	if dropped {
		if i := bytes.IndexByte(buf, '\n'); i >= 0 {
			buf = buf[i+1:]
		} else {
			buf = buf[:0]
		}
	}
	return buf, dropped, nil
}
