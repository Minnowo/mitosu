// Tests for reading the history back: hosts, series, and choosing and
// merging tiers for a range.

package store

import (
	"testing"
	"time"

	"mitosu/src/kanshi"
)

var memUsed = SeriesInfo{Kind: KindHost, Metric: "mem_used_bytes"}

func TestHostsAndSeries(t *testing.T) {
	db := openTemp(t)
	mustIngest(t, db, page(storeA, 0, 2, false, 0, baseSample(1, 5_000), baseSample(2, 15_000)))
	hosts, err := db.Hosts()
	if err != nil || len(hosts) != 1 || hosts[0].Name != "web" || hosts[0].LastSampleMs != 15_000 || hosts[0].Cursor.Next != 2 {
		t.Fatalf("hosts %+v, %v", hosts, err)
	}
	series, err := db.SeriesList("web")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range series {
		if s == memUsed {
			found = true
		}
	}
	if !found {
		t.Fatalf("mem_used_bytes not listed in %d series", len(series))
	}
	if _, err := db.Points("web", SeriesInfo{Kind: "host", Metric: "nope"}, 0, 1, 10, DefaultTiers, time.UnixMilli(0)); err != ErrNoSeries {
		t.Fatalf("unknown series: %v", err)
	}
}

func TestPointsChoosesAndMergesTiers(t *testing.T) {
	db := openTemp(t)
	// 2 hours of samples every 10 s: 720 raw values, 120 minute buckets.
	var samples []*kanshi.Sample
	for i := 0; i < 720; i++ {
		s := baseSample(uint64(i+1), uint64(i)*10_000)
		s.Mem.AvailableKiB = 8000 - uint64(i%60) // used grows within each 10 minutes
		samples = append(samples, s)
	}
	mustIngest(t, db, page(storeA, 0, 720, false, 0, samples...))
	now := time.UnixMilli(2 * 3_600_000)

	raw, err := db.Points("web", memUsed, 0, 7_200_000, 1000, DefaultTiers, now)
	if err != nil || raw.Resolution != 0 || raw.Step != 0 || len(raw.Points) != 720 {
		t.Fatalf("raw: res %d step %d points %d err %v", raw.Resolution, raw.Step, len(raw.Points), err)
	}
	merged, err := db.Points("web", memUsed, 0, 7_200_000, 100, DefaultTiers, now)
	if err != nil || merged.Step != 120_000 || len(merged.Points) != 60 {
		t.Fatalf("merged: res %d step %d points %d err %v", merged.Resolution, merged.Step, len(merged.Points), err)
	}
	// Merging keeps the true extremes and the sample-weighted average.
	var n int64
	minV, maxV, sum := merged.Points[0].Min, merged.Points[0].Max, 0.0
	for _, p := range merged.Points {
		n += p.N
		sum += p.Avg * float64(p.N)
		minV = min(minV, p.Min)
		maxV = max(maxV, p.Max)
	}
	if n != 720 || minV != 0 || maxV != 59*1024 {
		t.Fatalf("merged n %d min %v max %v", n, minV, maxV)
	}
	if avg := sum / float64(n); avg < 29.4*1024 || avg > 29.6*1024 {
		t.Fatalf("merged average %v", avg)
	}
}

func TestPointsReadsACoarserTierForLongRanges(t *testing.T) {
	db := openTemp(t)
	var samples []*kanshi.Sample
	for i := 0; i < 720; i++ {
		samples = append(samples, baseSample(uint64(i+1), uint64(i)*10_000))
	}
	mustIngest(t, db, page(storeA, 0, 720, false, 0, samples...))
	// 2 points wanted: reading 720 raw values (360 per point) is too much,
	// 120 minute buckets (60 per point) too, 2 hour buckets are not.
	res, err := db.Points("web", memUsed, 0, 7_200_000, 2, DefaultTiers, time.UnixMilli(7_200_000))
	if err != nil || res.Resolution != 3_600_000 || len(res.Points) != 2 {
		t.Fatalf("res %d step %d points %d err %v", res.Resolution, res.Step, len(res.Points), err)
	}
}

func TestPointsForUnalignedRanges(t *testing.T) {
	db := openTemp(t)
	var samples []*kanshi.Sample
	for i := 0; i < 720; i++ {
		samples = append(samples, baseSample(uint64(i+1), uint64(i)*10_000))
	}
	mustIngest(t, db, page(storeA, 0, 720, false, 0, samples...))
	now := time.UnixMilli(7_200_000)
	for _, r := range []struct{ from, to int64 }{{65_000, 6_065_000}, {30_000, 7_190_000}, {1, 7_199_999}, {59_999, 60_001}} {
		for _, max := range []int{1, 7, 100, 1000} {
			res, err := db.Points("web", memUsed, r.from, r.to, max, DefaultTiers, now)
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Points) > max {
				t.Errorf("[%d, %d) max %d: %d points", r.from, r.to, max, len(res.Points))
			}
			lowest := r.from
			if res.Resolution > 0 {
				lowest = r.from - r.from%res.Resolution
			}
			for _, p := range res.Points {
				if p.T < lowest || p.T >= r.to {
					t.Errorf("[%d, %d) max %d: point at %d outside the range", r.from, r.to, max, p.T)
				}
			}
		}
	}
}

func TestEventsLimitKeepsTheNewest(t *testing.T) {
	db := openTemp(t)
	for i := int64(1); i <= 5; i++ {
		if err := db.RecordPullStatus("web", i*1000, i%2 == 1, "x"); err != nil {
			t.Fatal(err)
		}
	}
	events, err := db.Events("web", 0, 10_000, 2)
	if err != nil || len(events) != 2 || events[0].TimeMs != 4000 || events[1].TimeMs != 5000 {
		t.Fatalf("events %+v, %v", events, err)
	}
}

func TestMergeStep(t *testing.T) {
	cases := []struct{ res, span, step int64 }{
		{0, 7_200_000, 120_000},
		{60_000, 7_200_000, 120_000},
		{420_000, 7_200_000, 840_000},
		{0, 1_000, 1_000},
		{3_600_000, 400 * 365 * 86_400_000, 64 * 30 * 86_400_000},
	}
	for _, c := range cases {
		if got := mergeStep(c.res, c.span, 100); got != c.step || (c.res > 0 && got%c.res != 0) {
			t.Errorf("mergeStep(%d, %d) = %d, want %d", c.res, c.span, got, c.step)
		}
	}
}

func TestPointsFallsBackToTheTierThatKeepsTheRange(t *testing.T) {
	db := openTemp(t)
	tiers := []Tier{{Resolution: 0, Keep: time.Hour}, {Resolution: time.Hour, Keep: 0}}
	mustIngestTiers(t, db, tiers, page(storeA, 0, 1, false, 0, baseSample(1, 1_000)))
	// A week later the raw tier no longer reaches back to the sample.
	res, err := db.Points("web", memUsed, 0, 3_600_000, 1000, tiers, time.UnixMilli(7*24*3_600_000))
	if err != nil || res.Resolution != 3_600_000 || len(res.Points) != 1 {
		t.Fatalf("res %d points %d err %v", res.Resolution, len(res.Points), err)
	}
}
