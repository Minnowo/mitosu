//go:build !unix

// File owners are not checked where they are not Unix user IDs.

package auth

import "os"

// checkOwner accepts every file on systems without Unix owners.
func checkOwner(path string, info os.FileInfo) error {
	return nil
}
