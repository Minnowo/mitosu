package ssh

import (
	"os/user"
	"path/filepath"
	"strings"
)


func ExpandPath(path string) string {

	usr, _ := user.Current()
	dir := usr.HomeDir

	if path == "~" {
		return dir
	} 

	if strings.HasPrefix(path, "~/") {
		return filepath.Join(dir, path[2:])
	}

	return path
}

