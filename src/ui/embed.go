// The built web UI (src/ui/dist, made by `make web`), embedded in the
// binary so mitosu serve needs no files beside it. The Go build fails
// with "pattern dist: no matching files found" until the UI is built.

package ui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Files returns the built UI, with index.html at its root.
func Files() (fs.FS, error) {
	return fs.Sub(dist, "dist")
}
