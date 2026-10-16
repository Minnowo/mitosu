// /api/points?host=&kind=&entity=&metric=&from=&to=&max=: one series'
// values over [from, to) (Unix milliseconds), at most max points
// (default 1000), from the finest tier that covers the range.

package web

import (
	"errors"
	"net/http"
	"strconv"

	"mitosu/src/store"
)

// maxRangeMs caps the requested range (100 years).
const maxRangeMs = 100 * 365 * 24 * 3600 * 1000

type pointsJSON struct {
	// Resolution is the tier read (ms; 0 raw); Step the width of each
	// point (ms; 0 means single raw values).
	Resolution int64 `json:"resolution"`
	Step       int64 `json:"step"`
	// Points are [t, avg, min, max, n].
	Points [][5]float64 `json:"points"`
}

func (s *Server) handlePoints(w http.ResponseWriter, r *http.Request) {
	host, ok := s.hostParam(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	from, err1 := strconv.ParseInt(q.Get("from"), 10, 64)
	to, err2 := strconv.ParseInt(q.Get("to"), 10, 64)
	if err1 != nil || err2 != nil || from < 0 || to <= from || to-from > maxRangeMs {
		writeError(w, http.StatusBadRequest, "want from < to, Unix milliseconds, at most 100 years apart")
		return
	}
	maxPoints := 1000
	if v := q.Get("max"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > store.MaxPoints {
			writeError(w, http.StatusBadRequest, "max must be 1 to "+strconv.Itoa(store.MaxPoints))
			return
		}
		maxPoints = n
	}
	series := store.SeriesInfo{Kind: q.Get("kind"), Entity: q.Get("entity"), Metric: q.Get("metric")}
	res, err := s.db.Points(host, series, from, to, maxPoints, s.opts.Tiers, s.now())
	if errors.Is(err, store.ErrNoSeries) {
		writeError(w, http.StatusNotFound, "no such series")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reading points failed")
		return
	}
	out := pointsJSON{Resolution: res.Resolution, Step: res.Step, Points: [][5]float64{}}
	for _, p := range res.Points {
		out.Points = append(out.Points, [5]float64{float64(p.T), p.Avg, p.Min, p.Max, float64(p.N)})
	}
	writeJSON(w, http.StatusOK, out)
}
