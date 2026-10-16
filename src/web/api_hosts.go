// /api/hosts: every configured host with its pull status and what the
// history holds for it.

package web

import (
	"net/http"
	"time"
)

// hostJSON is one host in /api/hosts. Times are Unix milliseconds, 0 for
// never.
type hostJSON struct {
	Name          string `json:"name"`
	Store         string `json:"store"`
	NextSeq       uint64 `json:"next_seq"`
	LastSampleMs  int64  `json:"last_sample_ms"`
	Pulling       bool   `json:"pulling"`
	LastAttemptMs int64  `json:"last_attempt_ms"`
	LastSuccessMs int64  `json:"last_success_ms"`
	LastError     string `json:"last_error"`
	LastSamples   int    `json:"last_samples"`
	NextPullMs    int64  `json:"next_pull_ms"`
}

func (s *Server) handleHosts(w http.ResponseWriter, r *http.Request) {
	stored, err := s.db.Hosts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reading hosts failed")
		return
	}
	byName := map[string]int{}
	for i, h := range stored {
		byName[h.Name] = i
	}
	out := []hostJSON{}
	for _, st := range s.sched.Status() {
		h := hostJSON{
			Name: st.Host, Pulling: st.Pulling, LastError: st.LastError, LastSamples: st.LastSamples,
			LastAttemptMs: ms(st.LastAttempt), LastSuccessMs: ms(st.LastSuccess), NextPullMs: ms(st.NextPull),
		}
		if i, ok := byName[st.Host]; ok {
			h.Store = stored[i].Cursor.Store
			h.NextSeq = stored[i].Cursor.Next
			if stored[i].LastSampleMs > 0 {
				h.LastSampleMs = stored[i].LastSampleMs
			}
		}
		out = append(out, h)
	}
	writeJSON(w, http.StatusOK, out)
}

// ms is t in Unix milliseconds, 0 for the zero time.
func ms(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}
