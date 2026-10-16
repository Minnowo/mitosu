// Tests for the newest-state queries behind the overview.

package store

import (
	"testing"
	"time"

	"mitosu/src/kanshi"
)

func TestLatestContainersAndProblems(t *testing.T) {
	db := openTemp(t)
	a := baseSample(1, 10_000)
	b := baseSample(2, 20_000)
	b.Mem.AvailableKiB = 1000
	*b.Errors = []kanshi.Problem{{Source: "fs", Message: "/boot/efi: statvfs failed"}}
	(*b.Containers)[0].State = "exited"
	(*b.Containers)[0].ExitCode = 3
	(*b.Containers)[0].Stats = nil
	mustIngest(t, db, page(storeA, 0, 2, false, 0, a, b))

	latest, err := db.Latest("web", 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, l := range latest {
		if l.SeriesInfo == memUsed {
			found = true
			if l.T != 20_000 || l.V != 7000*1024 {
				t.Fatalf("latest mem used %+v", l)
			}
		}
	}
	if !found {
		t.Fatal("mem_used_bytes missing from latest")
	}
	if stale, err := db.Latest("web", 30_000); err != nil || len(stale) != 0 {
		t.Fatalf("values older than the window: %d, %v", len(stale), err)
	}

	containers, err := db.Containers("web")
	if err != nil || len(containers) != 1 || containers[0].Name != "web" || containers[0].State != "exited" || containers[0].ExitCode != 3 {
		t.Fatalf("containers %+v, %v", containers, err)
	}
	problems, err := db.SampleProblems("web")
	if err != nil || len(problems) != 1 || problems[0] != "fs: /boot/efi: statvfs failed" {
		t.Fatalf("problems %q, %v", problems, err)
	}

	c := baseSample(3, 30_000) // problems gone
	mustIngest(t, db, page(storeA, 2, 3, false, 0, c))
	if problems, err := db.SampleProblems("web"); err != nil || len(problems) != 0 {
		t.Fatalf("problems after they cleared %q, %v", problems, err)
	}
	if containers, err := db.Containers("nope"); err != nil || containers != nil {
		t.Fatalf("unknown host: %v %v", containers, err)
	}
}

func TestProblemsFollowTheNewestSample(t *testing.T) {
	db := openTemp(t)
	a := baseSample(1, 10_000)
	*a.Errors = []kanshi.Problem{{Source: "fs", Message: "/boot/efi: statvfs failed"}}
	b := baseSample(2, 20_000)
	*b.Errors = []kanshi.Problem{{Source: "fs", Message: "/data: statvfs failed\nsecond line"}}
	mustIngest(t, db, page(storeA, 0, 2, false, 0, a, b))
	got, err := db.SampleProblems("web")
	if err != nil || len(got) != 1 || got[0] != "fs: /data: statvfs failed\nsecond line" {
		t.Fatalf("problems %q, %v (same source, new message)", got, err)
	}
}

func TestLatestWithoutRawTier(t *testing.T) {
	db := openTemp(t)
	tiers := []Tier{{Resolution: time.Minute, Keep: 0}}
	mustIngestTiers(t, db, tiers, page(storeA, 0, 2, false, 0, baseSample(1, 10_000), baseSample(2, 70_000)))
	latest, err := db.Latest("web", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range latest {
		if l.SeriesInfo == memUsed {
			if l.T != 60_000 {
				t.Fatalf("latest from rollups at %d, want the newest bucket 60000", l.T)
			}
			return
		}
	}
	t.Fatalf("no latest values without a raw tier: %d", len(latest))
}

func TestAverageAndSampleInterval(t *testing.T) {
	db := openTemp(t)
	var samples []*kanshi.Sample
	for i := 0; i < 30; i++ {
		s := baseSample(uint64(i+1), uint64(i)*10_000)
		s.Mem.AvailableKiB = 8000 - uint64(i*i) // used = i*i KiB: not linear, so wrong windows give other averages
		samples = append(samples, s)
	}
	mustIngest(t, db, page(storeA, 0, 30, false, 0, samples...))
	avg, ok, err := db.Average("web", memUsed, 100_000, 200_000) // samples 10..19: squares sum to 2185
	if err != nil || !ok || avg != 218.5*1024 {
		t.Fatalf("average %v %v %v", avg, ok, err)
	}
	if _, ok, err := db.Average("web", memUsed, 900_000, 1_000_000); err != nil || ok {
		t.Fatalf("empty range: %v %v", ok, err)
	}
	if iv, err := db.SampleInterval("web"); err != nil || iv != 10*time.Second {
		t.Fatalf("interval %v %v", iv, err)
	}
	// Without raw values only buckets wholly inside the range count.
	db2 := openTemp(t)
	mustIngestTiers(t, db2, []Tier{{Resolution: time.Minute}}, page(storeA, 0, 30, false, 0, samples...))
	avg, ok, err = db2.Average("web", memUsed, 60_000, 230_000) // buckets at 60 s and 120 s (180 s ends past the range), samples 6..17
	if err != nil || !ok || avg != 1730.0/12*1024 {             // squares of 6..17

		t.Fatalf("rollup average %v %v %v", avg, ok, err)
	}
}
