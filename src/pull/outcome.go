// Converts the result of a remote command into what the kanshi package
// needs to explain a failed request.

package pull

import (
	"mitosu/src/kanshi"
	"mitosu/src/ssh"
)

// Outcome returns the kanshi.Outcome of a kanshi request run by
// ssh.SSHClient.Exec.
func Outcome(res ssh.ExecResult) kanshi.Outcome {
	return kanshi.Outcome{
		ReadErr:  res.ReadErr,
		Status:   res.Status,
		Signal:   res.Signal,
		Stderr:   res.Stderr,
		TimedOut: res.TimedOut,
	}
}
