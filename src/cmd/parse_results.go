// Hands the output of one batch of remote commands to the stats that
// asked for them.

package cmd

import (
	"fmt"

	"mitosu/src/data"
	"mitosu/src/shell"
)

// parseResults gives each stat in stats its slice of results, in order:
// stat k gets the next stat.CmdCount(sh) outputs. It returns an error,
// without parsing anything, if results holds fewer outputs than the stats
// asked for (the remote shell stopped early, for example a failed sudo).
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
		stat.ParseCmdOutput(sh, results[i:i+n])
		i += n
	}
	return nil
}
