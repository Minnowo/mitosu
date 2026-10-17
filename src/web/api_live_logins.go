// GET /api/live/logins?host=&lines=&before=: SSH login attempts from the
// newest sshd journal lines.

package web

import (
	"net/http"

	"mitosu/src/live"
)

func (s *Server) handleLiveLogins(w http.ResponseWriter, r *http.Request, host string) {
	lines, ok := intParam(w, r, "lines", 500)
	if !ok {
		return
	}
	before := r.URL.Query().Get("before")
	res, err := live.Logins(r.Context(), s.opts.Live, s.opts.LiveOptions, host, int(min(lines, live.MaxLines+1)), before)
	liveAnswer(w, host, res, err)
}
