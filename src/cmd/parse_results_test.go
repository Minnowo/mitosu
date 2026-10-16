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
