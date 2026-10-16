// Tests for explaining failed kanshi requests to the user.

package kanshi

import (
	"errors"
	"strings"
	"testing"
)

func TestExplain(t *testing.T) {
	cases := []struct {
		name string
		res  Outcome
		want string
	}{
		{"ok", Outcome{}, ""},
		{"not installed", Outcome{Status: 127, Stderr: "sh: 1: kanshi: not found\n", ReadErr: ErrNoOutput}, "not installed"},
		{"nothing printed", Outcome{Status: 1, Stderr: "boom\x1b[2J", ReadErr: ErrNoOutput}, "exit 1: boom?[2J"},
		// What really happens with shell noise: the reader stops, Exec
		// cuts kanshi off, and kanshi exits non-zero (seen: 101).
		{"shell noise, cut off", Outcome{Status: 101, ReadErr: ErrNotKanshiOutput}, "shell startup files"},
		{"shell noise only", Outcome{ReadErr: ErrNotKanshiOutput}, "shell startup files"},
		{"server error", Outcome{Status: 1, ReadErr: &ServerError{Code: "no_store", Message: "no samples"}}, "kanshi error no_store"},
		{"truncated", Outcome{Status: -1, Signal: "KILL", ReadErr: ErrTruncated}, "killed by KILL"},
		{"timeout", Outcome{Status: -1, TimedOut: true, ReadErr: ErrTruncated}, "did not finish in time"},
	}
	for _, c := range cases {
		err := Explain(c.res)
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
	if !errors.Is(Explain(Outcome{ReadErr: ErrTruncated}), ErrTruncated) {
		t.Error("the reader error is not wrapped")
	}
}
