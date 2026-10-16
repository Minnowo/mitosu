// /api/series?host=: the series a host has (kind, entity, metric).

package web

import (
	"net/http"
)

type seriesJSON struct {
	Kind   string `json:"kind"`
	Entity string `json:"entity"`
	Metric string `json:"metric"`
}

func (s *Server) handleSeries(w http.ResponseWriter, r *http.Request) {
	host, ok := s.hostParam(w, r)
	if !ok {
		return
	}
	list, err := s.db.SeriesList(host)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reading series failed")
		return
	}
	out := []seriesJSON{}
	for _, x := range list {
		out = append(out, seriesJSON{Kind: x.Kind, Entity: x.Entity, Metric: x.Metric})
	}
	writeJSON(w, http.StatusOK, out)
}

// hostParam returns the host query parameter if it is a configured host,
// else writes 404 and returns false.
func (s *Server) hostParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	host := r.URL.Query().Get("host")
	for _, st := range s.sched.Status() {
		if st.Host == host {
			return host, true
		}
	}
	writeError(w, http.StatusNotFound, "no such host")
	return "", false
}
