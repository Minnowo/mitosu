//go:build unix

// Checks that a file belongs to the user running mitosu (Unix).

package auth

import (
	"fmt"
	"os"
	"syscall"
)

// checkOwner returns an error if info's file is not owned by this
// process's user.
func checkOwner(path string, info os.FileInfo) error {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("%s: cannot read its owner", path)
	}
	if int(st.Uid) != os.Getuid() {
		return fmt.Errorf("%s belongs to uid %d, not to you (uid %d); refusing to use it", path, st.Uid, os.Getuid())
	}
	return nil
}
