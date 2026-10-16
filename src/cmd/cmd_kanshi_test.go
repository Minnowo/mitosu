// Tests for explaining failed kanshi requests to the user.

package cmd

import (
	"errors"
	"strings"
	"testing"

	"mitosu/src/kanshi"
	"mitosu/src/ssh"
)

func TestKanshiResultError(t *testing.T) {
	cases := []struct {
		name string
		res  ssh.ExecResult
		want string
	}{
		{"ok", ssh.ExecResult{}, ""},
		{"not installed", ssh.ExecResult{Status: 127, Stderr: "sh: 1: kanshi: not found\n", ReadErr: kanshi.ErrNoOutput}, "not installed"},
		{"nothing printed", ssh.ExecResult{Status: 1, Stderr: "boom\x1b[2J", ReadErr: kanshi.ErrNoOutput}, "exit 1: boom?[2J"},
		// What really happens with shell noise: the reader stops, Exec
		// cuts kanshi off, and kanshi exits non-zero (seen: 101).
		{"shell noise, cut off", ssh.ExecResult{Status: 101, ReadErr: kanshi.ErrNotKanshiOutput}, "shell startup files"},
		{"shell noise only", ssh.ExecResult{ReadErr: kanshi.ErrNotKanshiOutput}, "shell startup files"},
		{"server error", ssh.ExecResult{Status: 1, ReadErr: &kanshi.ServerError{Code: "no_store", Message: "no samples"}}, "kanshi error no_store"},
		{"truncated", ssh.ExecResult{Status: -1, Signal: "KILL", ReadErr: kanshi.ErrTruncated}, "killed by KILL"},
		{"timeout", ssh.ExecResult{Status: -1, TimedOut: true, ReadErr: kanshi.ErrTruncated}, "did not finish in time"},
	}
	for _, c := range cases {
		err := kanshiResultError(c.res)
		if c.want == "" {
			if err != nil {
				t.Errorf("%s: %v", c.name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err %v, want it to contain %q", c.name, err, c.want)
		}
	}
	if !errors.Is(kanshiResultError(ssh.ExecResult{ReadErr: kanshi.ErrTruncated}), kanshi.ErrTruncated) {
		t.Error("the reader error is not wrapped")
	}
}
