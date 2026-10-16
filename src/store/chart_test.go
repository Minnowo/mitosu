// Tests for aligning series into one chart: mixed steps, gaps, empty and
// unknown series.

package store

import (
	"testing"
	"time"

	"mitosu/src/kanshi"
)

// TestChartMixedSteps is the review's case: one container ran 24 h, one
// only the last hour, so one is read in buckets and one raw.
func TestChartMixedSteps(t *testing.T) {
	db := openTemp(t)
	var samples []*kanshi.Sample
	day := 8640
	for i := 0; i < day; i++ {
		s := baseSample(uint64(i+1), uint64(i)*10_000)
		if i >= day-360 {
			late := (*s.Containers)[0]
			late.Name = "late"
			*s.Containers = append(*s.Containers, late)
		}
		samples = append(samples, s)
	}
	mustIngest(t, db, page(storeA, 0, uint64(day), false, 0, samples...))
	webMem := SeriesInfo{Kind: KindContainer, Entity: "web", Metric: "mem_used_bytes"}
	lateMem := SeriesInfo{Kind: KindContainer, Entity: "late", Metric: "mem_used_bytes"}
	to := int64(day) * 10_000
	res, err := db.Chart("web", []SeriesInfo{webMem, lateMem}, 0, to, 800, DefaultTiers, time.UnixMilli(to))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Series) != 2 || res.Step < 60_000 {
		t.Fatalf("series %d step %d", len(res.Series), res.Step)
	}
	webPoints, latePoints, gapRows := 0, 0, 0
	for j := range res.Times {
		if res.Values[0][j] != nil {
			webPoints++
		}
		if res.Values[1][j] != nil {
			latePoints++
		}
		if res.Values[0][j] == nil && res.Values[1][j] == nil {
			gapRows++
		}
	}
	if webPoints < len(res.Times)-1 || gapRows != 0 {
		t.Fatalf("the long series has %d of %d points and %d gap rows: it would draw no line", webPoints, len(res.Times), gapRows)
	}
	if latePoints == 0 || latePoints > len(res.Times)/20 {
		t.Fatalf("the short series has %d points", latePoints)
	}
}

func TestChartGapsAndMissingSeries(t *testing.T) {
	db := openTemp(t)
	var samples []*kanshi.Sample
	for i := 0; i < 60; i++ {
		ts := uint64(i) * 10_000
		if i >= 30 {
			ts += 600_000 // kanshi stopped for 10 minutes
		}
		samples = append(samples, baseSample(uint64(i+1), ts))
	}
	mustIngest(t, db, page(storeA, 0, 60, false, 0, samples...))
	unknown := SeriesInfo{Kind: KindHost, Metric: "nope"}
	res, err := db.Chart("web", []SeriesInfo{memUsed, unknown}, 0, 1_500_000, 800, DefaultTiers, time.UnixMilli(1_500_000))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Series) != 1 || res.Step != 10_000 {
		t.Fatalf("series %v step %d", res.Series, res.Step)
	}
	gaps := 0
	for j := range res.Times {
		if res.Values[0][j] == nil {
			gaps++
		}
	}
	if gaps != 1 || len(res.Times) != 61 {
		t.Fatalf("%d gap rows in %d times, want 1 in 61", gaps, len(res.Times))
	}
	if _, err := db.Chart("web", make([]SeriesInfo, MaxChartSeries+1), 0, 1, 10, DefaultTiers, time.UnixMilli(1)); err == nil {
		t.Fatal("too many series accepted")
	}
}
