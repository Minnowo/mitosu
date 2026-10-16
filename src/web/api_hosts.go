// /api/hosts: every configured host with its pull status, what the
// history holds for it, and the problems found (src/problems).

package web

import (
	"net/http"
	"time"

	"mitosu/src/problems"
	"mitosu/src/store"
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
	// PullIntervalMs is the configured time between pulls.
	PullIntervalMs int64              `json:"pull_interval_ms"`
	Problems       []problems.Problem `json:"problems"`
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
		h.PullIntervalMs = s.opts.PullInterval.Milliseconds()
		found, err := s.findProblems(st.Host, h.LastSampleMs, st.LastError)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "reading problems failed")
			return
		}
		h.Problems = found
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

// latestWindow is how far before the newest sample a value still counts
// as current.
const latestWindow = 30 * time.Minute

// findProblems gathers what src/problems needs about host. Windows are
// measured back from the newest sample, not from now: with long pull or
// sampling intervals "now" may be far past it (staleness is its own
// problem).
func (s *Server) findProblems(host string, lastSampleMs int64, lastError string) ([]problems.Problem, error) {
	in := problems.Input{Now: s.now(), PullInterval: s.opts.PullInterval, LastSampleMs: lastSampleMs, LastError: lastError}
	if lastSampleMs > 0 {
		latest, err := s.db.Latest(host, lastSampleMs-latestWindow.Milliseconds())
		if err != nil {
			return nil, err
		}
		in.Latest = latest
		busy := store.SeriesInfo{Kind: store.KindHost, Metric: "cpu_busy_pct"}
		avg, ok, err := s.db.Average(host, busy, lastSampleMs-(15*time.Minute).Milliseconds(), lastSampleMs+1)
		if err != nil {
			return nil, err
		}
		if ok {
			in.CPUBusy15m = &avg
		}
		if in.SampleInterval, err = s.db.SampleInterval(host); err != nil {
			return nil, err
		}
	}
	var err error
	if in.Containers, err = s.db.Containers(host); err != nil {
		return nil, err
	}
	if in.SampleProblems, err = s.db.SampleProblems(host); err != nil {
		return nil, err
	}
	found := problems.Find(in)
	if found == nil {
		found = []problems.Problem{}
	}
	return found, nil
}
