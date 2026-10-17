// /api/chart?host=&s=kind|entity|metric&s=...&from=&to=&max=: several
// series aligned on shared times for one chart (store.DB.Chart), up to
// store.MaxChartSeries of them. Series without values in the range are
// left out of the answer.

package web

import (
	"net/http"
	"strconv"
	"strings"

	"mitosu/src/store"
)

type chartJSON struct {
	Step   int64        `json:"step"`
	Times  []int64      `json:"times"`
	Series []seriesJSON `json:"series"`
	// Values[i][j] is series i at times[j], null where it has none.
	Values [][]*float64 `json:"values"`
}

func (s *Server) handleChart(w http.ResponseWriter, r *http.Request) {
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
	names := q["s"]
	if len(names) == 0 || len(names) > store.MaxChartSeries {
		writeError(w, http.StatusBadRequest, "want 1 to "+strconv.Itoa(store.MaxChartSeries)+" s=kind|entity|metric")
		return
	}
	var series []store.SeriesInfo
	for _, n := range names {
		// Kinds and metrics are fixed names without '|'; an entity (a
		// mount point, a container) may hold it.
		i, j := strings.Index(n, "|"), strings.LastIndex(n, "|")
		if i < 0 || i == j {
			writeError(w, http.StatusBadRequest, "want s=kind|entity|metric")
			return
		}
		series = append(series, store.SeriesInfo{Kind: n[:i], Entity: n[i+1 : j], Metric: n[j+1:]})
	}
	res, err := s.db.Chart(host, series, from, to, maxPoints, s.opts.Tiers, s.now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reading the chart failed")
		return
	}
	out := chartJSON{Step: res.Step, Times: res.Times, Series: []seriesJSON{}, Values: res.Values}
	if out.Times == nil {
		out.Times = []int64{}
	}
	if out.Values == nil {
		out.Values = [][]*float64{}
	}
	for _, x := range res.Series {
		out.Series = append(out.Series, seriesJSON{Kind: x.Kind, Entity: x.Entity, Metric: x.Metric})
	}
	writeJSON(w, http.StatusOK, out)
}
