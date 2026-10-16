// The newest known state of a host for the overview: each series' latest
// value, the containers' last known states and the problems the latest
// sample reported.

package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"mitosu/src/kanshi"
)

// LatestValue is the newest stored value of one series.
type LatestValue struct {
	SeriesInfo
	T int64
	V float64
}

// ContainerState is a container's last known state (as recorded for
// container events).
type ContainerState struct {
	Name      string `json:"name"`
	ID        string `json:"id"`
	Image     string `json:"image"`
	State     string `json:"state"`
	Health    string `json:"health,omitempty"`
	Restarts  uint64 `json:"restarts"`
	ExitCode  int64  `json:"exit_code"`
	OOMKilled bool   `json:"oom_killed"`
	StartedAt uint64 `json:"started_at_ms"`
}

// Latest returns the newest value of every series of host stored at or
// after sinceMs (older values are left out): the raw value, or where no
// raw tier is kept, the average of the newest bucket of the finest
// rollup tier stored.
func (db *DB) Latest(host string, sinceMs int64) ([]LatestValue, error) {
	out, err := db.latestQuery(`SELECT s.kind, s.entity, s.metric, p.t, p.v
		FROM series s JOIN hosts h ON h.id = s.host_id
		JOIN points p ON p.series_id = s.id
		WHERE h.name = ? AND p.t = (SELECT max(t) FROM points WHERE series_id = s.id) AND p.t >= ?
		ORDER BY s.kind, s.entity, s.metric`, host, sinceMs)
	if err != nil || len(out) > 0 {
		return out, err
	}
	return db.latestQuery(`SELECT s.kind, s.entity, s.metric, r.t, r.sum / r.n
		FROM series s JOIN hosts h ON h.id = s.host_id
		JOIN rollups r ON r.series_id = s.id
		WHERE h.name = ? AND r.resolution = (SELECT min(resolution) FROM rollups)
		AND r.t = (SELECT max(t) FROM rollups WHERE resolution = r.resolution AND series_id = s.id) AND r.t >= ?
		ORDER BY s.kind, s.entity, s.metric`, host, sinceMs)
}

// latestQuery runs a query of (kind, entity, metric, t, v) rows.
func (db *DB) latestQuery(query string, args ...any) ([]LatestValue, error) {
	rows, err := db.sql.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	defer rows.Close()
	var out []LatestValue
	for rows.Next() {
		var l LatestValue
		if err := rows.Scan(&l.Kind, &l.Entity, &l.Metric, &l.T, &l.V); err != nil {
			return nil, fmt.Errorf("store: %w", err)
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	return out, nil
}

// Containers returns host's containers with their last known states, by
// name (none for an unknown host).
func (db *DB) Containers(host string) ([]ContainerState, error) {
	var raw string
	err := db.sql.QueryRow("SELECT containers FROM hosts WHERE name = ?", host).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	var states map[string]string
	if err := json.Unmarshal([]byte(raw), &states); err != nil {
		return nil, fmt.Errorf("store: container states: %w", err)
	}
	out := []ContainerState{}
	for name, state := range states {
		var c ContainerState
		if err := json.Unmarshal([]byte(state), &c); err != nil {
			return nil, fmt.Errorf("store: container %s: %w", name, err)
		}
		c.Name = name
		out = append(out, c)
	}
	sortContainers(out)
	return out, nil
}

// SampleProblems returns the problems in host's newest stored sample,
// one "source: message" per entry (none if it reported none).
func (db *DB) SampleProblems(host string) ([]string, error) {
	var line string
	err := db.sql.QueryRow("SELECT last_sample FROM hosts WHERE name = ?", host).Scan(&line)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && line == "") {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	var sample kanshi.Sample
	if err := json.Unmarshal([]byte(line), &sample); err != nil {
		return nil, fmt.Errorf("store: newest sample: %w", err)
	}
	if sample.Errors == nil {
		return nil, nil
	}
	var out []string
	for _, p := range *sample.Errors {
		out = append(out, p.Source+": "+p.Message)
	}
	return out, nil
}

// Average returns the average of a series of host over [fromMs, toMs)
// from raw values, or where none are kept, from the rollup buckets of
// the finest tier that lie wholly inside the range. ok is false when
// there is no value.
func (db *DB) Average(host string, s SeriesInfo, fromMs, toMs int64) (float64, bool, error) {
	var avg sql.NullFloat64
	err := db.sql.QueryRow(`SELECT avg(p.v) FROM points p JOIN series s ON s.id = p.series_id JOIN hosts h ON h.id = s.host_id
		WHERE h.name = ? AND s.kind = ? AND s.entity = ? AND s.metric = ? AND p.t >= ? AND p.t < ?`,
		host, s.Kind, s.Entity, s.Metric, fromMs, toMs).Scan(&avg)
	if err != nil {
		return 0, false, fmt.Errorf("store: %w", err)
	}
	if avg.Valid {
		return avg.Float64, true, nil
	}
	err = db.sql.QueryRow(`SELECT sum(r.sum) / sum(r.n) FROM rollups r JOIN series s ON s.id = r.series_id JOIN hosts h ON h.id = s.host_id
		WHERE h.name = ? AND s.kind = ? AND s.entity = ? AND s.metric = ?
		AND r.resolution = (SELECT min(resolution) FROM rollups) AND r.t >= ? AND r.t + r.resolution <= ?`,
		host, s.Kind, s.Entity, s.Metric, fromMs, toMs).Scan(&avg)
	if err != nil {
		return 0, false, fmt.Errorf("store: %w", err)
	}
	return avg.Float64, avg.Valid, nil
}

// SampleInterval returns the median time between host's last 20 stored
// samples (kanshi's sampling interval), 0 if fewer than two are stored.
func (db *DB) SampleInterval(host string) (time.Duration, error) {
	rows, err := db.sql.Query(`SELECT s.t FROM samples s JOIN hosts h ON h.id = s.host_id
		WHERE h.name = ? ORDER BY s.t DESC LIMIT 20`, host)
	if err != nil {
		return 0, fmt.Errorf("store: %w", err)
	}
	defer rows.Close()
	var times []int64
	for rows.Next() {
		var t int64
		if err := rows.Scan(&t); err != nil {
			return 0, fmt.Errorf("store: %w", err)
		}
		times = append(times, t)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("store: %w", err)
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	return time.Duration(medianGap(times)) * time.Millisecond, nil
}

// sortContainers orders containers by name.
func sortContainers(cs []ContainerState) {
	sort.Slice(cs, func(i, j int) bool { return cs[i].Name < cs[j].Name })
}
