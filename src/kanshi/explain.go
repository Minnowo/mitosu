// Turns the outcome of a kanshi request run over SSH into an error a user
// can act on.

package kanshi

import (
	"errors"
	"fmt"
)

// Outcome is what is known about a finished kanshi request.
type Outcome struct {
	// ReadErr is the error of ReadExport or ReadVersion, nil on success.
	ReadErr error
	// Status is the command's exit status, -1 if it had none.
	Status int
	// Signal names the signal that ended the command, if any.
	Signal string
	// Stderr is the start of the command's stderr.
	Stderr string
	// TimedOut is true if the request was stopped at its deadline.
	TimedOut bool
}

// Explain explains a failed kanshi request from the reader's
// error and what the server said. kanshi output that is not kanshi's
// first line is reported as shell noise whatever the exit status: the
// reader stops at once and Exec then cuts kanshi off, so a non-zero status
// is expected there.
func Explain(res Outcome) error {
	if res.ReadErr == nil {
		return nil
	}
	exit := fmt.Sprintf("exit %d", res.Status)
	if res.Signal != "" {
		exit = "killed by " + Printable(res.Signal)
	}
	if res.Stderr != "" {
		exit += ": " + Printable(res.Stderr)
	}
	var serverErr *ServerError
	switch {
	case res.TimedOut:
		return fmt.Errorf("kanshi did not finish in time (%s): %w", exit, res.ReadErr)
	case errors.As(res.ReadErr, &serverErr):
		return res.ReadErr
	case errors.Is(res.ReadErr, ErrNoOutput) && res.Status == 127:
		return fmt.Errorf("kanshi is not installed on the server, or not on the login's PATH (%s)", exit)
	case errors.Is(res.ReadErr, ErrNoOutput):
		return fmt.Errorf("kanshi printed nothing (%s)", exit)
	case errors.Is(res.ReadErr, ErrNotKanshiOutput):
		return fmt.Errorf("%w: the login printed text before kanshi did; shell startup files (such as ~/.bashrc) must print to stderr or nothing (%s)", res.ReadErr, exit)
	}
	return fmt.Errorf("%w (%s)", res.ReadErr, exit)
}
