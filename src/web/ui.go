// Serves the built web UI's files. The UI needs no login to load: it
// holds no data, and it asks /api/session whether to show the login form.

package web

import (
	"io/fs"
	"net/http"
	"strings"
)

// fallback answers every request no other route takes: a JSON 404 for
// unknown API paths, 405 for methods other than GET and HEAD, and the UI's
// files (404 if there is no UI).
func fallback(ui fs.FS) http.Handler {
	var files http.Handler
	if ui != nil {
		files = uiHandler(noDirs{ui})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/"):
			writeError(w, http.StatusNotFound, "no such API")
		case r.Method != http.MethodGet && r.Method != http.MethodHead:
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		case files == nil:
			http.NotFound(w, r)
		default:
			files.ServeHTTP(w, r)
		}
	})
}

// uiHandler serves files from ui: index.html (never cached, so a new
// build is picked up) and the hashed files under assets/ (cached for a
// year, their names change with their content).
func uiHandler(ui fs.FS) http.Handler {
	files := http.FileServerFS(ui)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}

// noDirs hides every directory but the root, so the file server answers
// 404 instead of listing one (the root itself serves index.html).
type noDirs struct {
	fs.FS
}

func (n noDirs) Open(name string) (fs.File, error) {
	f, err := n.FS.Open(name)
	if err != nil || name == "." {
		return f, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if info.IsDir() {
		f.Close()
		return nil, fs.ErrNotExist
	}
	return f, nil
}
