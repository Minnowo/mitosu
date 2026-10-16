// /api/events?host=&from=&to=: a host's events in [from, to) (Unix
// milliseconds), oldest first; the newest maxEvents if there are more.

package web

import (
	"net/http"
	"strconv"
)

// maxEvents caps one response.
const maxEvents = 10000

type eventJSON struct {
	TimeMs  int64  `json:"t"`
	Kind    string `json:"kind"`
	Store   string `json:"store"`
	SeqFrom int64  `json:"seq_from"`
	SeqTo   int64  `json:"seq_to"`
	Subject string `json:"subject"`
	Detail  string `json:"detail"`
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	host, ok := s.hostParam(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	from, err1 := strconv.ParseInt(q.Get("from"), 10, 64)
	to, err2 := strconv.ParseInt(q.Get("to"), 10, 64)
	if err1 != nil || err2 != nil || to <= from {
		writeError(w, http.StatusBadRequest, "want from < to, Unix milliseconds")
		return
	}
	events, err := s.db.Events(host, from, to, maxEvents)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reading events failed")
		return
	}
	out := []eventJSON{}
	for _, e := range events {
		out = append(out, eventJSON{TimeMs: e.TimeMs, Kind: e.Kind, Store: e.Store, SeqFrom: e.SeqFrom, SeqTo: e.SeqTo, Subject: e.Subject, Detail: e.Detail})
	}
	writeJSON(w, http.StatusOK, out)
}
