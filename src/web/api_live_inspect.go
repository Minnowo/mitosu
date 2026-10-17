// GET /api/live/inspect?host=&container=: a container's configuration
// (ports, mounts, networks, restart policy, image; never its
// environment).

package web

import (
	"net/http"

	"mitosu/src/live"
)

func (s *Server) handleLiveInspect(w http.ResponseWriter, r *http.Request, host string) {
	container := r.URL.Query().Get("container")
	if !s.knownContainer(w, host, container) {
		return
	}
	res, err := live.Inspect(r.Context(), s.opts.Live, s.opts.LiveOptions, host, container)
	liveAnswer(w, host, res, err)
}
