// /api/containers?host=: each container's last known state.

package web

import "net/http"

func (s *Server) handleContainers(w http.ResponseWriter, r *http.Request) {
	host, ok := s.hostParam(w, r)
	if !ok {
		return
	}
	containers, err := s.db.Containers(host)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reading containers failed")
		return
	}
	writeJSON(w, http.StatusOK, containers)
}
