// GET /api/live/logs?host=&container=&lines=&before=: a container's
// newest log lines (before the page whose cursor is before).

package web

import (
	"net/http"

	"mitosu/src/live"
)

func (s *Server) handleLiveLogs(w http.ResponseWriter, r *http.Request, host string) {
	container := r.URL.Query().Get("container")
	lines, ok := intParam(w, r, "lines", 200)
	if !ok {
		return
	}
	before := r.URL.Query().Get("before")
	if !s.knownContainer(w, host, container) {
		return
	}
	res, err := live.Logs(r.Context(), s.opts.Live, s.opts.LiveOptions, host, container, int(min(lines, live.MaxLines+1)), before)
	liveAnswer(w, host, res, err)
}
