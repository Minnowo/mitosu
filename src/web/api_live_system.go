// GET /api/live/system?host=: OS, kernel, CPU, uptime, Docker version,
// upgradable packages, reboot-required and failed units.

package web

import (
	"net/http"

	"mitosu/src/live"
)

func (s *Server) handleLiveSystem(w http.ResponseWriter, r *http.Request, host string) {
	res, err := live.SystemView(r.Context(), s.opts.Live, s.opts.LiveOptions, host)
	liveAnswer(w, host, res, err)
}
