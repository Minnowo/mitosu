// Tests for pulling pages into the store from a fake kanshi that answers
// export requests like the real one: paging, a recreated server store,
// samples deleted before they were pulled, and failures.

package pull

import (
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"mitosu/src/ssh"
	"mitosu/src/store"
)

const (
	storeA = "0123456789abcdef0123456789abcdef"
	storeB = "fedcba9876543210fedcba9876543210"
)

func TestPullPagesUntilDone(t *testing.T) {
	k := &fakeKanshi{store: storeA, first: 1, last: 2500}
	db := openDB(t)
	res, err := Pull(k, db, "web", store.DefaultTiers, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if res.Samples != 2500 || res.Pages != 2 || res.Cursor != (store.Cursor{Store: storeA, Next: 2500}) {
		t.Fatalf("result %+v", res)
	}
	k.last = 2510
	res, err = Pull(k, db, "web", store.DefaultTiers, fixedNow)
	if err != nil || res.Samples != 10 || res.Pages != 1 {
		t.Fatalf("second pull %+v, %v", res, err)
	}
}

func TestPullAfterStoreRecreated(t *testing.T) {
	k := &fakeKanshi{store: storeA, first: 1, last: 10}
	db := openDB(t)
	if _, err := Pull(k, db, "web", store.DefaultTiers, fixedNow); err != nil {
		t.Fatal(err)
	}
	*k = fakeKanshi{store: storeB, first: 1, last: 3}
	res, err := Pull(k, db, "web", store.DefaultTiers, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if !res.StoreReset || res.Samples != 3 || res.Cursor != (store.Cursor{Store: storeB, Next: 3}) {
		t.Fatalf("result %+v", res)
	}
	// The first request after the change still used the old cursor (10).
	if k.requests[0] != 10 || k.requests[1] != 0 {
		t.Fatalf("requests since %v, want [10 0]", k.requests)
	}
}

func TestPullAfterSamplesWereDeleted(t *testing.T) {
	k := &fakeKanshi{store: storeA, first: 1, last: 10}
	db := openDB(t)
	if _, err := Pull(k, db, "web", store.DefaultTiers, fixedNow); err != nil {
		t.Fatal(err)
	}
	// mitosu was away: samples up to 50 were deleted, 51..60 remain.
	k.first, k.last = 51, 60
	res, err := Pull(k, db, "web", store.DefaultTiers, fixedNow)
	if err != nil || res.Samples != 10 || res.Cursor.Next != 60 {
		t.Fatalf("result %+v, %v", res, err)
	}
	events := eventRanges(t, db)
	if events["lost"] != "11-50" || events["seq_gap"] != "" {
		t.Fatalf("events %v, want lost 11-50 and no seq_gap", events)
	}
}

func TestPullFailureStoresNothing(t *testing.T) {
	k := &fakeKanshi{fail: true}
	db := openDB(t)
	_, err := Pull(k, db, "web", store.DefaultTiers, fixedNow)
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("err %v", err)
	}
	if c, _ := db.Cursor("web"); c != (store.Cursor{}) {
		t.Fatalf("cursor %+v after a failed pull", c)
	}
}

// fakeKanshi answers `kanshi export --since N --limit M --gzip` from a
// store holding samples first..last; samples below first were deleted.
type fakeKanshi struct {
	store       string
	first, last uint64
	fail        bool
	requests    []uint64
}

func (k *fakeKanshi) Exec(command string, timeout time.Duration, read func(io.Reader) error) (ssh.ExecResult, error) {
	if k.fail {
		return ssh.ExecResult{Status: 127, Stderr: "sh: kanshi: not found", ReadErr: read(strings.NewReader(""))}, nil
	}
	var since, limit uint64
	if _, err := fmt.Sscanf(command, "kanshi export --since %d --limit %d --gzip", &since, &limit); err != nil {
		return ssh.ExecResult{}, fmt.Errorf("unexpected command %q", command)
	}
	k.requests = append(k.requests, since)

	var b strings.Builder
	b.WriteString(`{"type":"header","kanshi":"1.0.0","format":"1.0","store":"` + k.store + `"}` + "\n")
	lost := uint64(0)
	if k.first > since+1 {
		lost = k.first - 1
	}
	next := since
	sent := uint64(0)
	for seq := max(since+1, k.first); seq <= k.last && sent < limit; seq++ {
		fmt.Fprintf(&b, `{"type":"sample","seq":%d,"time_ms":%d,"boot_id":"63269f57-5133-4a1a-a9b7-2d6868e7ae87",`+
			`"mem":{"total_kib":8000,"free_kib":5000,"available_kib":6000,"buffers_kib":0,"cached_kib":0,"swap_total_kib":0,"swap_free_kib":0},"errors":[]}`+"\n",
			seq, 1_700_000_000_000+seq*10_000)
		next = seq
		sent++
	}
	more := sent == limit && next < k.last
	b.WriteString(`{"type":"end","next":` + strconv.FormatUint(next, 10) + `,"more":` + strconv.FormatBool(more) +
		`,"lost_through":` + strconv.FormatUint(lost, 10) + "}\n")
	return ssh.ExecResult{ReadErr: read(strings.NewReader(b.String()))}, nil
}

func fixedNow() time.Time {
	return time.UnixMilli(1_700_000_000_000 + 100_000*10_000)
}

func openDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "mitosu.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// eventRanges returns "from-to" of each event kind (the last of a kind).
func eventRanges(t *testing.T, db *store.DB) map[string]string {
	t.Helper()
	events, err := db.Events("web", 0, 1<<62, 100000)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, e := range events {
		out[e.Kind] = fmt.Sprintf("%d-%d", e.SeqFrom, e.SeqTo)
	}
	return out
}
