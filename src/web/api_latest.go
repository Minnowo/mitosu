// /api/latest?host=: the newest value of each of a host's series (from
// the 30 minutes up to its newest sample), for the overview.

package web

import (
	"net/http"

	"mitosu/src/store"
)

type latestJSON struct {
	Kind   string  `json:"kind"`
	Entity string  `json:"entity"`
	Metric string  `json:"metric"`
	T      int64   `json:"t"`
	V      float64 `json:"v"`
}

func (s *Server) handleLatest(w http.ResponseWriter, r *http.Request) {
	host, ok := s.hostParam(w, r)
	if !ok {
		return
	}
	info, err := s.hostInfo(host)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reading host failed")
		return
	}
	values, err := s.db.Latest(host, info.LastSampleMs-latestWindow.Milliseconds())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reading latest values failed")
		return
	}
	out := []latestJSON{}
	for _, v := range values {
		out = append(out, latestJSON{Kind: v.Kind, Entity: v.Entity, Metric: v.Metric, T: v.T, V: v.V})
	}
	writeJSON(w, http.StatusOK, out)
}

// hostInfo returns what the database knows of host (zero for none).
func (s *Server) hostInfo(host string) (store.HostInfo, error) {
	hosts, err := s.db.Hosts()
	if err != nil {
		return store.HostInfo{}, err
	}
	for _, h := range hosts {
		if h.Name == host {
			return h, nil
		}
	}
	return store.HostInfo{}, nil
}
