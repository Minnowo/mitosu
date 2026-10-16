// Stores pulled kanshi samples: one page (one export response) per
// transaction, together with the host's cursor, so a page is stored
// whole or not at all and never twice.

package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/rs/zerolog/log"

	"mitosu/src/kanshi"
)

// Event kinds.
const (
	// EventSeqGap: sequence numbers were skipped (kanshi restarted after
	// a crash); not data loss by itself.
	EventSeqGap = "seq_gap"
	// EventLost: kanshi deleted samples before mitosu pulled them.
	EventLost = "lost"
	// EventStoreReset: the server's store was recreated.
	EventStoreReset = "store_reset"
	// EventBoot: the server rebooted (new boot_id).
	EventBoot = "boot"
	// EventErrors: the set of problems a sample reports changed.
	EventErrors = "errors"
	// EventContainer: a container appeared, changed state, health, image,
	// ID, restart count or exit, or disappeared.
	EventContainer = "container"
	// EventPullFailed: pulling the host started failing (detail: why).
	EventPullFailed = "pull_failed"
	// EventPullOK: pulling the host works again after failures.
	EventPullOK = "pull_ok"
)

// ErrStoreChanged is returned by Ingest when the server's store ID is not
// the one the cursor belongs to; call ResetStore and pull again from 0.
var ErrStoreChanged = errors.New("server store changed")

// Cursor is where the next pull of a host starts.
type Cursor struct {
	// Store is the server store the cursor belongs to; "" before the
	// first pull.
	Store string
	// Next is the value for kanshi export --since.
	Next uint64
}

// PulledSample is a checked sample and its line as kanshi sent it.
type PulledSample struct {
	Sample kanshi.Sample
	Line   []byte
}

// Page is one export response.
type Page struct {
	Header  kanshi.Header
	Since   uint64
	Samples []PulledSample
	End     kanshi.End
	// PulledAtMs is when the page was pulled (Unix milliseconds of this
	// machine), the time of events that no sample dates.
	PulledAtMs uint64
}

// Cursor returns the cursor of host, the zero Cursor for a new host.
func (db *DB) Cursor(host string) (Cursor, error) {
	var c Cursor
	var next int64
	err := db.sql.QueryRow("SELECT store, next_seq FROM hosts WHERE name = ?", host).Scan(&c.Store, &next)
	if errors.Is(err, sql.ErrNoRows) {
		return Cursor{}, nil
	}
	if err != nil {
		return Cursor{}, fmt.Errorf("store: %w", err)
	}
	c.Next = uint64(next)
	return c, nil
}

// ResetStore records that host's server store is now store (at time t,
// milliseconds of this machine) and restarts its cursor at 0. Data
// already stored stays, and so do the last known boot ID, problems and
// container states, so the new store's first sample only adds events for
// what really changed.
func (db *DB) ResetStore(host, store string, t uint64) error {
	tx, err := db.sql.Begin()
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	defer rollback(tx)
	hostID, err := hostRow(tx, host)
	if err != nil {
		return err
	}
	var old string
	if err := tx.QueryRow("SELECT store FROM hosts WHERE id = ?", hostID).Scan(&old); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	if err := addEvent(tx, hostID, t, EventStoreReset, store, 0, 0, "", "was "+old); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE hosts SET store = ?, next_seq = 0, last_sample = '' WHERE id = ?", store, hostID); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	return tx.Commit()
}

// hostState is what samples of a host are compared with, kept in its
// hosts row between pulls.
type hostState struct {
	store      string
	next       uint64
	lastLine   string
	lastTime   int64
	bootID     string
	problems   string
	containers map[string]string
}

// Ingest stores one page for host with tiers (see ValidateTiers) and
// moves the cursor past it. The page must start where the cursor is
// (page.Since == cursor Next) and come from the cursor's store, or from
// any store for a new host; a different store gives ErrStoreChanged.
// It returns the number of samples stored.
func (db *DB) Ingest(host string, tiers []Tier, page Page) (int, error) {
	tx, err := db.sql.Begin()
	if err != nil {
		return 0, fmt.Errorf("store: %w", err)
	}
	defer rollback(tx)

	hostID, err := hostRow(tx, host)
	if err != nil {
		return 0, err
	}
	st, err := loadState(tx, hostID)
	if err != nil {
		return 0, err
	}
	if st.store != "" && st.store != page.Header.Store {
		return 0, ErrStoreChanged
	}
	if st.next != page.Since {
		return 0, fmt.Errorf("store: page starts after %d, cursor is at %d", page.Since, st.next)
	}

	var prev *kanshi.Sample
	if st.lastLine != "" {
		var s kanshi.Sample
		if err := json.Unmarshal([]byte(st.lastLine), &s); err != nil {
			log.Warn().Err(err).Str("host", host).Msg("Stored last sample is unreadable; rates restart")
		} else {
			prev = &s
		}
	}

	w, err := db.newWriter(tx, hostID, tiers)
	if err != nil {
		return 0, err
	}
	defer w.close()

	lost := page.End.LostThrough
	for i := range page.Samples {
		cur := &page.Samples[i].Sample
		if err := w.events(page, &st, prev, cur); err != nil {
			return 0, err
		}
		// A sample not newer than the last stored values (the server
		// clock went back) adds no values: the raw tier keeps one value
		// per time, and rollups must count the same samples.
		if int64(cur.TimeMs) > st.lastTime {
			if err := w.values(derive(prev, cur), int64(cur.TimeMs)); err != nil {
				return 0, err
			}
			st.lastTime = int64(cur.TimeMs)
		}
		if _, err := tx.Exec("INSERT OR IGNORE INTO samples (host_id, t, seq) VALUES (?, ?, ?)", hostID, int64(cur.TimeMs), int64(cur.Seq)); err != nil {
			return 0, fmt.Errorf("store: %w", err)
		}
		prev = cur
		st.lastLine = string(page.Samples[i].Line)
	}

	if lost > page.Since {
		// The deleted samples are those after the last returned sample
		// below lost (kanshi may report a loss in the middle of a page).
		from := page.Since + 1
		t := page.PulledAtMs
		for _, ps := range page.Samples {
			if ps.Sample.Seq < lost && ps.Sample.Seq+1 > from {
				from = ps.Sample.Seq + 1
			}
		}
		for _, ps := range page.Samples {
			if ps.Sample.Seq > lost {
				t = ps.Sample.TimeMs
				break
			}
		}
		if from <= lost {
			detail := fmt.Sprintf("samples %d to %d were deleted on the server before they were pulled", from, lost)
			if err := addEvent(tx, hostID, t, EventLost, page.Header.Store, from, lost, "", detail); err != nil {
				return 0, err
			}
		}
	}

	// The cursor also passes lost samples: they are gone, and kanshi
	// would otherwise report the same loss on every pull.
	st.next = page.End.Next
	if lost > st.next {
		st.next = lost
	}
	st.store = page.Header.Store
	if err := saveState(tx, hostID, st); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: %w", err)
	}
	db.mu.Lock()
	for k, id := range w.newSeries {
		db.series[k] = id
	}
	db.mu.Unlock()
	return len(page.Samples), nil
}

// loadState reads a host's state.
func loadState(tx *sql.Tx, hostID int64) (hostState, error) {
	var st hostState
	var next int64
	var containers string
	err := tx.QueryRow("SELECT store, next_seq, last_sample, last_time, boot_id, problems, containers FROM hosts WHERE id = ?", hostID).
		Scan(&st.store, &next, &st.lastLine, &st.lastTime, &st.bootID, &st.problems, &containers)
	if err != nil {
		return hostState{}, fmt.Errorf("store: %w", err)
	}
	st.next = uint64(next)
	st.containers = map[string]string{}
	if err := json.Unmarshal([]byte(containers), &st.containers); err != nil {
		return hostState{}, fmt.Errorf("store: container states: %w", err)
	}
	return st, nil
}

// saveState writes a host's state. Values are bounded by the kanshi
// reader (seq and time fit in an int64).
func saveState(tx *sql.Tx, hostID int64, st hostState) error {
	if st.next > math.MaxInt64 {
		return fmt.Errorf("store: cursor %d out of range", st.next)
	}
	containers, err := json.Marshal(st.containers)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	_, err = tx.Exec(`UPDATE hosts SET store = ?, next_seq = ?, last_sample = ?, last_time = ?, boot_id = ?, problems = ?, containers = ?
		WHERE id = ?`, st.store, int64(st.next), st.lastLine, st.lastTime, st.bootID, st.problems, string(containers), hostID)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	return nil
}

// writer writes one page's values and events inside a transaction.
type writer struct {
	db        *DB
	tx        *sql.Tx
	hostID    int64
	raw       bool
	rollups   []int64
	newSeries map[seriesKey]int64
	point     *sql.Stmt
	rollup    *sql.Stmt
}

func (db *DB) newWriter(tx *sql.Tx, hostID int64, tiers []Tier) (*writer, error) {
	w := &writer{db: db, tx: tx, hostID: hostID, newSeries: map[seriesKey]int64{}}
	for _, t := range tiers {
		if t.Resolution == 0 {
			w.raw = true
		} else {
			w.rollups = append(w.rollups, t.Resolution.Milliseconds())
		}
	}
	var err error
	w.point, err = tx.Prepare("INSERT OR REPLACE INTO points (series_id, t, v) VALUES (?, ?, ?)")
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	w.rollup, err = tx.Prepare(`INSERT INTO rollups (resolution, series_id, t, n, sum, min, max) VALUES (?, ?, ?, 1, ?, ?, ?)
		ON CONFLICT (resolution, series_id, t) DO UPDATE SET
		n = n + 1, sum = sum + excluded.sum, min = min(min, excluded.min), max = max(max, excluded.max)`)
	if err != nil {
		w.point.Close()
		return nil, fmt.Errorf("store: %w", err)
	}
	return w, nil
}

func (w *writer) close() {
	for _, s := range []*sql.Stmt{w.point, w.rollup} {
		if err := s.Close(); err != nil {
			log.Debug().Err(err).Msg("Closing statement")
		}
	}
}

// values writes one sample's values at time t (milliseconds) to the raw
// tier and to each rollup bucket that holds t. A series repeated in one
// sample (duplicate names from the server) keeps its first value.
func (w *writer) values(values []value, t int64) error {
	seen := map[seriesKey]bool{}
	for _, v := range values {
		if math.IsNaN(v.v) || math.IsInf(v.v, 0) {
			continue
		}
		key := seriesKey{hostID: w.hostID, kind: v.kind, entity: v.entity, metric: v.metric}
		if seen[key] {
			continue
		}
		seen[key] = true
		id, err := w.seriesID(key)
		if err != nil {
			return err
		}
		if w.raw {
			if _, err := w.point.Exec(id, t, compact(v.v)); err != nil {
				return fmt.Errorf("store: %w", err)
			}
		}
		for _, res := range w.rollups {
			bucket := t - mod(t, res)
			if _, err := w.rollup.Exec(res, id, bucket, v.v, v.v, v.v); err != nil {
				return fmt.Errorf("store: %w", err)
			}
		}
	}
	return nil
}

// seriesID returns the ID of a series, creating it if needed.
func (w *writer) seriesID(key seriesKey) (int64, error) {
	w.db.mu.Lock()
	id, ok := w.db.series[key]
	w.db.mu.Unlock()
	if ok {
		return id, nil
	}
	if id, ok := w.newSeries[key]; ok {
		return id, nil
	}
	if _, err := w.tx.Exec("INSERT OR IGNORE INTO series (host_id, kind, entity, metric) VALUES (?, ?, ?, ?)",
		w.hostID, key.kind, key.entity, key.metric); err != nil {
		return 0, fmt.Errorf("store: %w", err)
	}
	if err := w.tx.QueryRow("SELECT id FROM series WHERE host_id = ? AND kind = ? AND entity = ? AND metric = ?",
		w.hostID, key.kind, key.entity, key.metric).Scan(&id); err != nil {
		return 0, fmt.Errorf("store: %w", err)
	}
	// Cached in the DB only after commit: a rolled-back series is gone.
	w.newSeries[key] = id
	return id, nil
}

// events records what changed with cur: sequence numbers skipped that no
// reported loss explains, a reboot, a different set of failing sources,
// container changes. st holds the last known boot, problems and
// containers and is updated.
func (w *writer) events(page Page, st *hostState, prev, cur *kanshi.Sample) error {
	t := cur.TimeMs
	store := page.Header.Store
	if prev != nil {
		// Numbers up to the cursor (which passes earlier losses) or up to
		// this page's reported loss were deleted, not skipped.
		base := prev.Seq
		if page.Since > base {
			base = page.Since
		}
		if lost := page.End.LostThrough; lost < cur.Seq && lost > base {
			base = lost
		}
		if cur.Seq > base+1 {
			detail := fmt.Sprintf("kanshi skipped samples %d to %d (restart after a crash)", base+1, cur.Seq-1)
			if err := addEvent(w.tx, w.hostID, t, EventSeqGap, store, base+1, cur.Seq-1, "", detail); err != nil {
				return err
			}
		}
	}
	if cur.BootID != nil {
		if st.bootID != "" && st.bootID != *cur.BootID {
			if err := addEvent(w.tx, w.hostID, t, EventBoot, store, cur.Seq, cur.Seq, "", *cur.BootID); err != nil {
				return err
			}
		}
		st.bootID = *cur.BootID
	}
	if sources := problemSources(cur); sources != st.problems {
		if err := addEvent(w.tx, w.hostID, t, EventErrors, store, cur.Seq, cur.Seq, "", problemText(cur)); err != nil {
			return err
		}
		st.problems = sources
	}
	return w.containerEvents(store, st, cur)
}

// containerEvents records containers that appeared, changed or went away
// compared with their last known states in st, and updates st. An absent
// containers section says nothing. A container missing from a list that
// may be partial (a containers problem was reported, or the list is at
// the protocol's cap) is not taken as gone.
func (w *writer) containerEvents(store string, st *hostState, cur *kanshi.Sample) error {
	if cur.Containers == nil {
		return nil
	}
	seen := map[string]bool{}
	for _, c := range *cur.Containers {
		seen[c.Name] = true
		state := containerState(c)
		if st.containers[c.Name] == state {
			continue
		}
		if err := addEvent(w.tx, w.hostID, cur.TimeMs, EventContainer, store, cur.Seq, cur.Seq, c.Name, state); err != nil {
			return err
		}
		st.containers[c.Name] = state
	}
	partial := len(*cur.Containers) >= 64
	for _, p := range *cur.Errors {
		if p.Source == "containers" {
			partial = true
		}
	}
	if partial {
		return nil
	}
	var gone []string
	for name := range st.containers {
		if !seen[name] {
			gone = append(gone, name)
		}
	}
	sort.Strings(gone)
	for _, name := range gone {
		if err := addEvent(w.tx, w.hostID, cur.TimeMs, EventContainer, store, cur.Seq, cur.Seq, name, "gone"); err != nil {
			return err
		}
		delete(st.containers, name)
	}
	return nil
}

// containerState is the part of a container whose change is an event.
func containerState(c kanshi.Container) string {
	b, err := json.Marshal(struct {
		ID        string `json:"id"`
		Image     string `json:"image"`
		State     string `json:"state"`
		Health    string `json:"health,omitempty"`
		Restarts  uint64 `json:"restarts"`
		ExitCode  int64  `json:"exit_code"`
		OOMKilled bool   `json:"oom_killed"`
		StartedAt uint64 `json:"started_at_ms"`
	}{c.ID, c.Image, c.State, c.Health, c.Restarts, c.ExitCode, c.OOMKilled, c.StartedAtMs})
	if err != nil {
		return ""
	}
	return string(b)
}

// problemSources is the sorted set of a sample's failing sources: an
// event is recorded when it changes, not when only a message's wording
// does.
func problemSources(s *kanshi.Sample) string {
	set := map[string]bool{}
	for _, p := range *s.Errors {
		set[p.Source] = true
	}
	var sources []string
	for src := range set {
		sources = append(sources, src)
	}
	sort.Strings(sources)
	return strings.Join(sources, ",")
}

// problemText is a sample's problems as text, one per line.
func problemText(s *kanshi.Sample) string {
	var parts []string
	for _, p := range *s.Errors {
		parts = append(parts, p.Source+": "+p.Message)
	}
	return strings.Join(parts, "\n")
}

// addEvent records an event once: repeating the same kind, store, seq
// range and subject is ignored.
func addEvent(tx *sql.Tx, hostID int64, t uint64, kind, store string, seqFrom, seqTo uint64, subject, detail string) error {
	if t > math.MaxInt64 || seqFrom > math.MaxInt64 || seqTo > math.MaxInt64 {
		return fmt.Errorf("store: event values out of range")
	}
	_, err := tx.Exec(`INSERT OR IGNORE INTO events (host_id, t, kind, store, seq_from, seq_to, subject, detail)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, hostID, int64(t), kind, store, int64(seqFrom), int64(seqTo), subject, detail)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	return nil
}

// hostRow returns the ID of host, creating its row if needed.
func hostRow(tx *sql.Tx, host string) (int64, error) {
	if _, err := tx.Exec("INSERT OR IGNORE INTO hosts (name) VALUES (?)", host); err != nil {
		return 0, fmt.Errorf("store: %w", err)
	}
	var id int64
	if err := tx.QueryRow("SELECT id FROM hosts WHERE name = ?", host).Scan(&id); err != nil {
		return 0, fmt.Errorf("store: %w", err)
	}
	return id, nil
}

// compact returns v as an int64 when it is a whole number that fits,
// which SQLite stores in fewer bytes than a REAL.
func compact(v float64) any {
	if v == math.Trunc(v) && math.Abs(v) < 1<<53 {
		return int64(v)
	}
	return v
}

// mod is a non-negative remainder.
func mod(a, b int64) int64 {
	m := a % b
	if m < 0 {
		m += b
	}
	return m
}

// rollback undoes an unfinished transaction; after Commit it does nothing.
func rollback(tx *sql.Tx) {
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		log.Warn().Err(err).Msg("Rolling back a transaction")
	}
}
