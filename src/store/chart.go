// Several series of one host over a range, on one shared time grid for
// one chart. Each series is read with Points; when their steps differ
// (a short-lived container read raw next to long-lived ones read in
// buckets), all are merged into the coarsest step so they share times.
// Where nothing was stored for more than gapSteps steps, a time with no
// values is inserted so a chart draws a gap instead of a line. Series
// with no value in the range are left out.

package store

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

const (
	// MaxChartSeries caps the series of one chart.
	MaxChartSeries = 64
	// gapSteps is how many steps without values make a gap.
	gapSteps = 2.5
)

// ChartResult is a chart's data.
type ChartResult struct {
	// Step is the width of one point in milliseconds, or for raw values
	// the usual distance between samples.
	Step int64
	// Times are point times (ms), ascending.
	Times []int64
	// Series are the series that have values, in request order.
	Series []SeriesInfo
	// Values[i][j] is series i at Times[j], nil where it has none.
	Values [][]*float64
}

// Chart returns the series of host over [fromMs, toMs), at most
// maxPoints times (before gap markers), aligned as described in the file
// header. Unknown series are left out like empty ones.
func (db *DB) Chart(host string, series []SeriesInfo, fromMs, toMs int64, maxPoints int, tiers []Tier, now time.Time) (ChartResult, error) {
	if len(series) > MaxChartSeries {
		return ChartResult{}, fmt.Errorf("store: %d series in one chart, at most %d", len(series), MaxChartSeries)
	}
	type loaded struct {
		info SeriesInfo
		res  PointsResult
	}
	var all []loaded
	step := int64(0)
	for _, s := range series {
		res, err := db.Points(host, s, fromMs, toMs, maxPoints, tiers, now)
		if errors.Is(err, ErrNoSeries) {
			continue
		}
		if err != nil {
			return ChartResult{}, err
		}
		if len(res.Points) == 0 {
			continue
		}
		all = append(all, loaded{s, res})
		step = max(step, res.Step)
	}
	out := ChartResult{}
	if len(all) == 0 {
		return out, nil
	}

	// Values by time: raw values at their own times when every series is
	// raw, else every series merged into buckets of the coarsest step.
	type acc struct{ sum, n float64 }
	grid := map[int64][]acc{}
	base := fromMs
	if step > 0 {
		base = fromMs - mod(fromMs, step)
	}
	for i, l := range all {
		for _, p := range l.res.Points {
			t := p.T
			if step > 0 {
				t = base + (p.T-base)/step*step
			}
			row, ok := grid[t]
			if !ok {
				row = make([]acc, len(all))
				grid[t] = row
			}
			n := float64(max(p.N, 1))
			row[i].sum += p.Avg * n
			row[i].n += n
		}
	}
	times := make([]int64, 0, len(grid))
	for t := range grid {
		times = append(times, t)
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	if step == 0 {
		step = medianGap(times)
	}

	out.Step = step
	for _, l := range all {
		out.Series = append(out.Series, l.info)
		out.Values = append(out.Values, nil)
	}
	for k, t := range times {
		if k > 0 && step > 0 && float64(t-times[k-1]) > gapSteps*float64(step) {
			out.Times = append(out.Times, times[k-1]+step)
			for i := range all {
				out.Values[i] = append(out.Values[i], nil)
			}
		}
		out.Times = append(out.Times, t)
		row := grid[t]
		for i := range all {
			if row[i].n == 0 {
				out.Values[i] = append(out.Values[i], nil)
				continue
			}
			v := row[i].sum / row[i].n
			out.Values[i] = append(out.Values[i], &v)
		}
	}
	return out, nil
}

// medianGap is the median distance between consecutive times, 0 if
// there are fewer than two.
func medianGap(times []int64) int64 {
	if len(times) < 2 {
		return 0
	}
	gaps := make([]int64, 0, len(times)-1)
	for i := 1; i < len(times); i++ {
		gaps = append(gaps, times[i]-times[i-1])
	}
	sort.Slice(gaps, func(i, j int) bool { return gaps[i] < gaps[j] })
	return gaps[len(gaps)/2]
}
