// Tests for storing pulled pages in a real SQLite file: cursor, tiers,
// events, store resets, lost samples, retention and the file itself.

package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mitosu/src/kanshi"
)

const (
	storeA = "0123456789abcdef0123456789abcdef"
	storeB = "fedcba9876543210fedcba9876543210"
)

func TestIngestStoresTiersAndMovesCursor(t *testing.T) {
	db := openTemp(t)
	a := baseSample(1, 60_000)
	b := baseSample(2, 70_000)
	b.CPU.Total.Idle += 100
	n, err := db.Ingest("web", DefaultTiers, page(storeA, 0, 2, false, 0, a, b))
	if err != nil || n != 2 {
		t.Fatalf("ingest: %d, %v", n, err)
	}
	c, err := db.Cursor("web")
	if err != nil || c != (Cursor{Store: storeA, Next: 2}) {
		t.Fatalf("cursor %+v, %v", c, err)
	}
	if got := count(t, db, "SELECT count(*) FROM points p JOIN series s ON s.id = p.series_id WHERE s.kind = 'host' AND s.metric = 'mem_used_bytes'"); got != 2 {
		t.Fatalf("raw mem points: %d", got)
	}
	if got := count(t, db, "SELECT count(*) FROM points p JOIN series s ON s.id = p.series_id WHERE s.metric = 'cpu_busy_pct'"); got != 1 {
		t.Fatalf("raw cpu points: %d, want 1 (rates need two samples)", got)
	}
	// Both samples fall in the 60_000 minute bucket and the 0 hour bucket.
	for _, res := range []int64{60_000, 3_600_000} {
		var n, sum, min, max float64
		err := db.sql.QueryRow(`SELECT r.n, r.sum, r.min, r.max FROM rollups r JOIN series s ON s.id = r.series_id
			WHERE s.kind = 'host' AND s.metric = 'mem_used_bytes' AND r.resolution = ?`, res).Scan(&n, &sum, &min, &max)
		if err != nil || n != 2 || sum != 2*2000*1024 || min != 2000*1024 || max != 2000*1024 {
			t.Fatalf("rollup %d: n %v sum %v min %v max %v err %v", res, n, sum, min, max, err)
		}
	}
}

func TestRatesContinueAcrossPages(t *testing.T) {
	db := openTemp(t)
	a := baseSample(1, 0)
	b := baseSample(2, 10_000)
	b.CPU.Total.Idle += 100
	mustIngest(t, db, page(storeA, 0, 1, false, 0, a))
	mustIngest(t, db, page(storeA, 1, 2, false, 0, b))
	if got := count(t, db, "SELECT count(*) FROM points p JOIN series s ON s.id = p.series_id WHERE s.metric = 'cpu_busy_pct'"); got != 1 {
		t.Fatalf("cpu points: %d, want the rate between the pages", got)
	}
}

func TestPageMustStartAtTheCursor(t *testing.T) {
	db := openTemp(t)
	mustIngest(t, db, page(storeA, 0, 1, false, 0, baseSample(1, 0)))
	if _, err := db.Ingest("web", DefaultTiers, page(storeA, 0, 1, false, 0, baseSample(1, 0))); err == nil {
		t.Fatal("the same page was stored twice")
	}
	if got := count(t, db, "SELECT count(*) FROM samples"); got != 1 {
		t.Fatalf("samples: %d", got)
	}
}

func TestStoreChange(t *testing.T) {
	db := openTemp(t)
	mustIngest(t, db, page(storeA, 0, 5, false, 0, baseSample(5, 0)))
	_, err := db.Ingest("web", DefaultTiers, page(storeB, 5, 1, false, 0, baseSample(1, 10_000)))
	if !errors.Is(err, ErrStoreChanged) {
		t.Fatalf("err %v", err)
	}
	if err := db.ResetStore("web", storeB, 10_000); err != nil {
		t.Fatal(err)
	}
	if c, _ := db.Cursor("web"); c != (Cursor{Store: storeB, Next: 0}) {
		t.Fatalf("cursor after reset %+v", c)
	}
	mustIngest(t, db, page(storeB, 0, 1, false, 0, baseSample(1, 10_000)))
	if got := count(t, db, "SELECT count(*) FROM events WHERE kind = 'store_reset'"); got != 1 {
		t.Fatalf("store_reset events: %d", got)
	}
}

func TestLostSamplesAreNotACrash(t *testing.T) {
	db := openTemp(t)
	mustIngest(t, db, page(storeA, 0, 2, false, 0, baseSample(1, 0), baseSample(2, 10_000)))
	// Samples 3..9 were deleted and nothing newer exists: kanshi keeps
	// next at 2 and reports the loss.
	mustIngest(t, db, page(storeA, 2, 2, false, 9))
	c, _ := db.Cursor("web")
	if c.Next != 9 {
		t.Fatalf("cursor %d, want 9 (past the lost samples)", c.Next)
	}
	mustIngest(t, db, page(storeA, 9, 10, false, 0, baseSample(10, 20_000)))
	events := kinds(t, db)
	if events[EventLost] != "3-9" || events[EventSeqGap] != "" {
		t.Fatalf("events %v, want lost 3-9 and no seq_gap", events)
	}
}

func TestLossInTheMiddleOfAPage(t *testing.T) {
	db := openTemp(t)
	mustIngest(t, db, page(storeA, 0, 6, false, 5, baseSample(1, 0), baseSample(2, 10_000), baseSample(6, 20_000)))
	events := kinds(t, db)
	if events[EventLost] != "3-5" || events[EventSeqGap] != "" {
		t.Fatalf("events %v, want lost 3-5 and no seq_gap", events)
	}
}

func TestCrashGapAfterALoss(t *testing.T) {
	db := openTemp(t)
	// 3..5 deleted, 7 skipped by a crash.
	mustIngest(t, db, page(storeA, 0, 8, false, 5, baseSample(1, 0), baseSample(2, 10_000), baseSample(6, 20_000), baseSample(8, 30_000)))
	events := kinds(t, db)
	if events[EventLost] != "3-5" || events[EventSeqGap] != "7-7" {
		t.Fatalf("events %v", events)
	}
}

func TestEvents(t *testing.T) {
	db := openTemp(t)
	a := baseSample(1, 0)
	b := baseSample(4, 10_000) // 2 and 3 skipped
	other := "11111111-2222-3333-4444-555555555555"
	b.BootID = &other
	*b.Errors = []kanshi.Problem{{Source: "fs", Message: "statvfs failed"}}
	(*b.Containers)[0].State = "exited"
	(*b.Containers)[0].Stats = nil
	c := baseSample(5, 20_000)
	c.BootID = &other
	*c.Containers = nil
	mustIngest(t, db, page(storeA, 0, 5, false, 0, a, b, c))
	for kind, want := range map[string]int64{EventSeqGap: 1, EventBoot: 1, EventErrors: 2, EventContainer: 3} {
		if got := count(t, db, "SELECT count(*) FROM events WHERE kind = ?", kind); got != want {
			t.Errorf("%s events: %d, want %d", kind, got, want)
		}
	}
	var detail string
	if err := db.sql.QueryRow("SELECT detail FROM events WHERE kind = 'container' ORDER BY t DESC LIMIT 1").Scan(&detail); err != nil || detail != "gone" {
		t.Fatalf("last container event %q, %v", detail, err)
	}
}

func TestContainerEventsAgainstLastKnownState(t *testing.T) {
	db := openTemp(t)
	a := baseSample(1, 0)
	db1 := (*a.Containers)[0]
	db1.Name = "db"
	*a.Containers = append(*a.Containers, db1)
	b := baseSample(2, 10_000)
	b.Containers = nil // Docker unreachable: says nothing
	c := baseSample(3, 20_000)
	*c.Errors = []kanshi.Problem{{Source: "containers", Message: "db: not reached in time"}} // partial list
	d := baseSample(4, 30_000)                                                               // complete list without db
	mustIngest(t, db, page(storeA, 0, 4, false, 0, a, b, c, d))
	got := containerEvents(t, db)
	want := []string{"1 web new", "1 db new", "4 db gone"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("container events %v, want %v", got, want)
	}
	// A recreated server store does not announce the same containers again.
	if err := db.ResetStore("web", storeB, 40_000); err != nil {
		t.Fatal(err)
	}
	mustIngest(t, db, page(storeB, 0, 1, false, 0, baseSample(1, 50_000)))
	if got := containerEvents(t, db); len(got) != 3 {
		t.Fatalf("container events after a store reset: %v", got)
	}
}

func TestRebootSeenAcrossSampleWithoutBootID(t *testing.T) {
	db := openTemp(t)
	a := baseSample(1, 0)
	b := baseSample(2, 10_000)
	b.BootID = nil
	c := baseSample(3, 20_000)
	other := "11111111-2222-3333-4444-555555555555"
	c.BootID = &other
	mustIngest(t, db, page(storeA, 0, 3, false, 0, a, b, c))
	if got := count(t, db, "SELECT count(*) FROM events WHERE kind = 'boot'"); got != 1 {
		t.Fatalf("boot events %d, want 1", got)
	}
}

func TestRollupMath(t *testing.T) {
	db := openTemp(t)
	var samples []*kanshi.Sample
	for i, avail := range []uint64{7000, 5000, 6000} { // used 1000, 3000, 2000 KiB
		s := baseSample(uint64(i+1), uint64(i)*10_000)
		s.Mem.AvailableKiB = avail
		samples = append(samples, s)
	}
	mustIngest(t, db, page(storeA, 0, 3, false, 0, samples...))
	var n, sum, min, max float64
	err := db.sql.QueryRow(`SELECT r.n, r.sum, r.min, r.max FROM rollups r JOIN series s ON s.id = r.series_id
		WHERE s.kind = 'host' AND s.metric = 'mem_used_bytes' AND r.resolution = 60000`).Scan(&n, &sum, &min, &max)
	if err != nil || n != 3 || sum != 6000*1024 || min != 1000*1024 || max != 3000*1024 {
		t.Fatalf("n %v sum %v min %v max %v err %v", n, sum, min, max, err)
	}
}

func TestClockGoingBackKeepsTiersConsistent(t *testing.T) {
	db := openTemp(t)
	a := baseSample(1, 30_000)
	b := baseSample(2, 30_000) // same time
	b.Mem.AvailableKiB = 1000
	c := baseSample(3, 20_000) // earlier
	mustIngest(t, db, page(storeA, 0, 3, false, 0, a, b, c))
	raw := count(t, db, "SELECT count(*) FROM points p JOIN series s ON s.id = p.series_id WHERE s.kind = 'host' AND s.metric = 'mem_used_bytes'")
	var n int64
	if err := db.sql.QueryRow(`SELECT sum(r.n) FROM rollups r JOIN series s ON s.id = r.series_id
		WHERE s.kind = 'host' AND s.metric = 'mem_used_bytes' AND r.resolution = 60000`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if raw != 1 || n != 1 {
		t.Fatalf("raw points %d, rollup count %d, want 1 and 1", raw, n)
	}
}

func TestConcurrentIngest(t *testing.T) {
	db := openTemp(t)
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			for seq := uint64(1); seq <= 20; seq++ {
				if _, err := db.Ingest(host, DefaultTiers, page(storeA, seq-1, seq, false, 0, baseSample(seq, seq*10_000))); err != nil {
					errs <- err
					return
				}
			}
		}(fmt.Sprintf("host%d", i))
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if got := count(t, db, "SELECT count(*) FROM samples"); got != 80 {
		t.Fatalf("samples %d, want 80", got)
	}
}

// kinds returns "from-to" of the last event of each kind.
func kinds(t *testing.T, db *DB) map[string]string {
	t.Helper()
	events, err := db.Events("web", math.MinInt64, math.MaxInt64)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, e := range events {
		out[e.Kind] = fmt.Sprintf("%d-%d", e.SeqFrom, e.SeqTo)
	}
	return out
}

// containerEvents lists container events as "seq name new|gone|changed".
func containerEvents(t *testing.T, db *DB) []string {
	t.Helper()
	events, err := db.Events("web", math.MinInt64, math.MaxInt64)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	seen := map[string]bool{}
	for _, e := range events {
		if e.Kind != EventContainer {
			continue
		}
		what := "changed"
		switch {
		case e.Detail == "gone":
			what = "gone"
		case !seen[e.Subject]:
			what = "new"
		}
		seen[e.Subject] = true
		out = append(out, fmt.Sprintf("%d %s %s", e.SeqFrom, e.Subject, what))
	}
	return out
}

func TestGoldenPage(t *testing.T) {
	data, err := os.ReadFile("../../testdata/kanshi-golden/export-host-basic.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.ReplaceAll(data, []byte(`"STORE"`), []byte(`"`+storeA+`"`))
	var samples []PulledSample
	header, end, err := kanshi.ReadExport(bytes.NewReader(data), 0, 0, func(s kanshi.Sample, line []byte) error {
		samples = append(samples, PulledSample{Sample: s, Line: append([]byte(nil), line...)})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	db := openTemp(t)
	if _, err := db.Ingest("web", DefaultTiers, Page{Header: header, Since: 0, Samples: samples, End: end}); err != nil {
		t.Fatal(err)
	}
	if got := count(t, db, "SELECT count(*) FROM series WHERE kind = 'container'"); got == 0 {
		t.Fatal("no container series from the golden page")
	}
}

func TestPrune(t *testing.T) {
	db := openTemp(t)
	day := int64(24 * time.Hour / time.Millisecond)
	tiers := []Tier{{Resolution: 0, Keep: 2 * 24 * time.Hour}, {Resolution: time.Hour, Keep: 10 * 24 * time.Hour}}
	old := baseSample(1, uint64(1*day))
	recent := baseSample(2, uint64(9*day))
	mustIngestTiers(t, db, tiers, page(storeA, 0, 2, false, 0, old, recent))
	if _, err := db.Prune(tiers, time.UnixMilli(10*day)); err != nil {
		t.Fatal(err)
	}
	if got := count(t, db, "SELECT count(*) FROM points WHERE t < ?", 8*day); got != 0 {
		t.Fatalf("old raw points left: %d", got)
	}
	if got := count(t, db, "SELECT count(*) FROM points WHERE t = ?", 9*day); got == 0 {
		t.Fatal("recent raw points deleted")
	}
	if got := count(t, db, "SELECT count(*) FROM rollups WHERE t = ?", 1*day); got == 0 {
		t.Fatal("hour buckets within their keep were deleted")
	}
	if got := count(t, db, "SELECT count(*) FROM samples"); got != 1 {
		t.Fatalf("samples left: %d, want 1", got)
	}
	if _, err := db.Prune(tiers, time.UnixMilli(12*day)); err != nil {
		t.Fatal(err)
	}
	if got := count(t, db, "SELECT count(*) FROM rollups WHERE t = ?", 1*day); got != 0 {
		t.Fatal("expired hour buckets kept")
	}
}

func TestValidateTiers(t *testing.T) {
	bad := [][]Tier{
		nil,
		{{Resolution: time.Minute, Keep: time.Hour}, {Resolution: 0, Keep: 2 * time.Hour}},
		{{Resolution: time.Minute}, {Resolution: time.Hour, Keep: time.Hour}},
		{{Resolution: time.Hour, Keep: time.Hour}, {Resolution: time.Minute, Keep: 2 * time.Hour}},
		{{Resolution: 90 * time.Second, Keep: time.Hour}, {Resolution: 2 * time.Minute, Keep: 2 * time.Hour}},
		{{Resolution: 0, Keep: 2 * time.Hour}, {Resolution: time.Minute, Keep: time.Hour}},
		{{Resolution: 1500 * time.Millisecond}},
	}
	for i, tiers := range bad {
		if err := ValidateTiers(tiers); err == nil {
			t.Errorf("case %d accepted: %+v", i, tiers)
		}
	}
	if err := ValidateTiers(DefaultTiers); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTiers([]Tier{{Resolution: 0, Keep: 0}}); err != nil {
		t.Fatalf("raw forever: %v", err)
	}
}

func TestDatabaseFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new")
	path := filepath.Join(dir, "mitosu.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	for p, want := range map[string]os.FileMode{dir: 0o700, path: 0o600} {
		info, err := os.Stat(p)
		if err != nil || info.Mode().Perm() != want {
			t.Errorf("%s: mode %v, want %v (%v)", p, info.Mode().Perm(), want, err)
		}
	}
	db, err = Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if _, err := db.sql.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := Open(path); err == nil {
		t.Fatal("a newer schema was opened")
	}
}

func TestOddPathIsTheFileUsed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a%41b?c#d.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	mustIngest(t, db, page(storeA, 0, 1, false, 0, baseSample(1, 0)))
	db.Close()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "a%41b?c#d.db") {
			t.Fatalf("SQLite wrote %s, not the named file", e.Name())
		}
		info, err := e.Info()
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s: mode %v (%v)", e.Name(), info.Mode().Perm(), err)
		}
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		t.Fatalf("named file is empty: %v %v", info, err)
	}
}

func TestOtherSQLiteFileIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "other.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.Exec("PRAGMA user_version = 0"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := Open(path); err == nil {
		t.Fatal("a database with tables but no mitosu version was opened")
	}
}

func openTemp(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "mitosu.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// page builds an export page; lines are encoded from the samples.
func page(store string, since, next uint64, more bool, lost uint64, samples ...*kanshi.Sample) Page {
	p := Page{
		Header:     kanshi.Header{Kanshi: "1.0.0", Format: "1.0", Store: store},
		Since:      since,
		End:        kanshi.End{Next: next, More: more, LostThrough: lost},
		PulledAtMs: 1,
	}
	for _, s := range samples {
		p.Samples = append(p.Samples, PulledSample{Sample: *s, Line: mustJSON(s)})
	}
	return p
}

func mustIngest(t *testing.T, db *DB, p Page) {
	t.Helper()
	mustIngestTiers(t, db, DefaultTiers, p)
}

func mustIngestTiers(t *testing.T, db *DB, tiers []Tier, p Page) {
	t.Helper()
	if _, err := db.Ingest("web", tiers, p); err != nil {
		t.Fatal(err)
	}
}

func count(t *testing.T, db *DB, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.sql.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func mustJSON(s *kanshi.Sample) []byte {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return b
}
