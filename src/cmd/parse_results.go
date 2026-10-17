// Hands the output of one batch of remote commands to the stats that
// asked for them.

package cmd

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"mitosu/src/data"
	"mitosu/src/shell"
)

// parseResults gives each stat in stats its slice of results, in order:
// stat k gets the next stat.CmdCount(sh) outputs. It returns an error,
// without parsing anything, if results holds fewer outputs than the stats
// asked for (the remote shell stopped early, for example a failed sudo).
// Control characters and invalid UTF-8 in the output are replaced (see
// terminalSafe), so nothing a server prints can drive the terminal.
func parseResults(sh shell.ShellType, stats []data.SystemStat, results []string) error {
	want := 0
	for _, stat := range stats {
		want += stat.CmdCount(sh)
	}
	if len(results) < want {
		return fmt.Errorf("remote shell returned %d of %d command outputs", len(results), want)
	}
	i := 0
	for _, stat := range stats {
		n := stat.CmdCount(sh)
		outs := make([]string, n)
		for j := range outs {
			outs[j] = terminalSafe(results[i+j])
		}
		stat.ParseCmdOutput(sh, outs)
		i += n
	}
	return nil
}

// terminalSafe returns s with \r, \v and \f replaced by a space (white
// space to the parsers), and other control characters except newline and
// tab, and invalid UTF-8, replaced by '?'.
func terminalSafe(s string) string {
	var b strings.Builder
	for len(s) > 0 {
		r, size := utf8.DecodeRuneInString(s)
		switch {
		case r == utf8.RuneError && size <= 1:
			b.WriteByte('?')
		case r == '\r' || r == '\v' || r == '\f':
			b.WriteByte(' ')
		case unicode.IsControl(r) && r != '\n' && r != '\t':
			b.WriteByte('?')
		default:
			b.WriteString(s[:size])
		}
		s = s[size:]
	}
	return b.String()
}
