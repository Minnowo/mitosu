// The remote commands for kanshi requests (../kanshi/docs/protocol.md,
// "Requests"). Only numbers go into them, so they are safe for any login
// shell.

package kanshi

import (
	"fmt"
	"strconv"
)

// MaxLimit is the largest --limit kanshi accepts.
const MaxLimit = 1000000

// ExportCommand returns `kanshi export --since since [--limit limit]
// [--gzip]`; limit 0 means no --limit. It returns an error if limit is
// above MaxLimit.
func ExportCommand(since, limit uint64, gzip bool) (string, error) {
	if limit > MaxLimit {
		return "", fmt.Errorf("limit %d is above %d", limit, MaxLimit)
	}
	cmd := "kanshi export --since " + strconv.FormatUint(since, 10)
	if limit > 0 {
		cmd += " --limit " + strconv.FormatUint(limit, 10)
	}
	if gzip {
		cmd += " --gzip"
	}
	return cmd, nil
}

// VersionCommand returns `kanshi version`.
func VersionCommand() string {
	return "kanshi version"
}
