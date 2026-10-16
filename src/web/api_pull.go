// POST /api/pull?host=: pull a host now instead of waiting.

package web

import "net/http"

func (s *Server) handlePull(w http.ResponseWriter, r *http.Request) {
	host, ok := s.hostParam(w, r)
	if !ok {
		return
	}
	if err := s.sched.PullNow(host); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
