// Tests for handing remote command output to the stats that asked for it.

package cmd

import (
	"testing"

	"mitosu/src/data"
	"mitosu/src/shell"
)

func TestParseResultsWithMissingOutputIsAnError(t *testing.T) {
	stats := []data.SystemStat{&data.ProcInfoSystemStat{}, &data.FSSystemStat{}}
	// 5 proc commands and 1 df were sent; the output stopped after 2.
	err := parseResults(shell.PosixShellType, stats, []string{"web1\n", "1.0 1.0\n"})
	if err == nil {
		t.Fatal("no error for missing output")
	}
}

// Server output reaches the terminal: control sequences in it (here an
// OSC 52 clipboard write in the hostname) must not.
func TestParseResultsDropsControlCharacters(t *testing.T) {
	proc := &data.ProcInfoSystemStat{}
	stats := []data.SystemStat{proc}
	results := make([]string, proc.CmdCount(shell.PosixShellType))
	results[0] = "web\x1b]52;c;ZXZpbA==\x07\u009b2J\xff-\u00e9\n"
	if err := parseResults(shell.PosixShellType, stats, results); err != nil {
		t.Fatal(err)
	}
	if want := "web?]52;c;ZXZpbA==??2J?-\u00e9"; proc.Hostname != want {
		t.Fatalf("hostname %q, want %q", proc.Hostname, want)
	}
}

// \r, \v and \f stay white space for the parsers (CRLF output).
func TestTerminalSafeKeepsWhiteSpace(t *testing.T) {
	if got := terminalSafe("1.0 2.0\r\n3\v4\f5\x00"); got != "1.0 2.0 \n3 4 5?" {
		t.Fatalf("got %q", got)
	}
}
