// Reads the history back for the web UI: hosts, their series, and a
// series' values over a time range from the finest tier that covers it,
// merged into at most a given number of points.

package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// MaxPoints caps the points one Points call returns.
const MaxPoints = 5000

// HostInfo is a host as the database knows it.
type HostInfo struct {
	Name   string
	Cursor Cursor
	// LastSampleMs is the newest stored sample's time, -1 if none.
	LastSampleMs int64
}

// SeriesInfo names one series of a host.
type SeriesInfo struct {
	Kind, Entity, Metric string
}

// Point is one merged value: the average, minimum and maximum of N values
// whose times fall in [T, T+step).
type Point struct {
	T             int64
	Avg, Min, Max float64
	N             int64
}

// PointsResult is a series' values over a range.
type PointsResult struct {
	// Resolution is the tier read, in milliseconds; 0 is the raw tier.
	Resolution int64
	// Step is the width of each point in milliseconds; 0 means raw values
	// were returned one by one.
	Step   int64
	Points []Point
}

// ErrNoSeries is returned by Points for a series the host does not have.
var ErrNoSeries = errors.New("no such series")

// Hosts returns every host in the database, by name.
func (db *DB) Hosts() ([]HostInfo, error) {
	rows, err := db.sql.Query(`SELECT h.name, h.store, h.next_seq,
		coalesce((SELECT max(s.t) FROM samples s WHERE s.host_id = h.id), -1)
		FROM hosts h ORDER BY h.name`)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	defer rows.Close()
	var out []HostInfo
	for rows.Next() {
		var h HostInfo
		var next int64
		if err := rows.Scan(&h.Name, &h.Cursor.Store, &next, &h.LastSampleMs); err != nil {
			return nil, fmt.Errorf("store: %w", err)
		}
		h.Cursor.Next = uint64(next)
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	return out, nil
}

// SeriesList returns host's series, ordered by kind, entity and metric.
func (db *DB) SeriesList(host string) ([]SeriesInfo, error) {
	rows, err := db.sql.Query(`SELECT s.kind, s.entity, s.metric FROM series s JOIN hosts h ON h.id = s.host_id
		WHERE h.name = ? ORDER BY s.kind, s.entity, s.metric`, host)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	defer rows.Close()
	var out []SeriesInfo
	for rows.Next() {
		var s SeriesInfo
		if err := rows.Scan(&s.Kind, &s.Entity, &s.Metric); err != nil {
			return nil, fmt.Errorf("store: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	return out, nil
}

// Points returns the values of one series of host with fromMs <= t <
// toMs (for a rollup tier, from the start of the bucket holding fromMs),
// at most maxPoints of them (1..MaxPoints). tiers are the configured
// tiers and now the current time: the finest tier that still holds fromMs
// and is not too fine for the range is read (see chooseTier), and its
// values are merged into equal steps if there are more than maxPoints.
func (db *DB) Points(host string, s SeriesInfo, fromMs, toMs int64, maxPoints int, tiers []Tier, now time.Time) (PointsResult, error) {
	if maxPoints < 1 || maxPoints > MaxPoints {
		return PointsResult{}, fmt.Errorf("store: max points %d out of 1..%d", maxPoints, MaxPoints)
	}
	if toMs <= fromMs {
		return PointsResult{}, fmt.Errorf("store: empty time range")
	}
	var id int64
	err := db.sql.QueryRow(`SELECT s.id FROM series s JOIN hosts h ON h.id = s.host_id
		WHERE h.name = ? AND s.kind = ? AND s.entity = ? AND s.metric = ?`, host, s.Kind, s.Entity, s.Metric).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return PointsResult{}, ErrNoSeries
	}
	if err != nil {
		return PointsResult{}, fmt.Errorf("store: %w", err)
	}

	tier, err := db.chooseTier(id, fromMs, toMs, maxPoints, tiers, now.UnixMilli())
	if err != nil {
		return PointsResult{}, err
	}
	res := tier.Resolution.Milliseconds()
	// Points are counted from base: fromMs for raw values, else the start
	// of the tier bucket holding fromMs (a bucket cannot be split, so the
	// first point may hold values from up to one resolution before
	// fromMs). Steps from base keep the number of points at or below
	// maxPoints for any fromMs.
	base := fromMs
	if res > 0 {
		base = fromMs - mod(fromMs, res)
	}
	step := int64(0)
	if res > 0 || tier.count > int64(maxPoints) {
		step = mergeStep(res, toMs-base, maxPoints)
	}
	out := PointsResult{Resolution: res, Step: step}
	var rows *sql.Rows
	switch {
	case res == 0 && step == 0:
		rows, err = db.sql.Query(`SELECT t, v, v, v, 1 FROM points WHERE series_id = ? AND t >= ? AND t < ? ORDER BY t`, id, fromMs, toMs)
	case res == 0:
		rows, err = db.sql.Query(`SELECT ? + ((t - ?) / ?) * ? AS b, avg(v), min(v), max(v), count(*) FROM points
			WHERE series_id = ? AND t >= ? AND t < ? GROUP BY b ORDER BY b`, base, base, step, step, id, fromMs, toMs)
	default:
		rows, err = db.sql.Query(`SELECT ? + ((t - ?) / ?) * ? AS b, sum(sum) / sum(n), min(min), max(max), sum(n) FROM rollups
			WHERE resolution = ? AND series_id = ? AND t >= ? AND t < ? GROUP BY b ORDER BY b`, base, base, step, step, res, id, base, toMs)
	}
	if err != nil {
		return PointsResult{}, fmt.Errorf("store: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p Point
		if err := rows.Scan(&p.T, &p.Avg, &p.Min, &p.Max, &p.N); err != nil {
			return PointsResult{}, fmt.Errorf("store: %w", err)
		}
		out.Points = append(out.Points, p)
	}
	if err := rows.Err(); err != nil {
		return PointsResult{}, fmt.Errorf("store: %w", err)
	}
	return out, nil
}

// chosenTier is the tier Points reads and, for the raw tier, how many
// values the range holds.
type chosenTier struct {
	Tier
	count int64
}

// readFactor bounds how many stored rows Points reads per point it
// returns: a finer tier is preferred and merged, unless that means more
// than readFactor rows per point.
const readFactor = 20

// chooseTier picks, from fine to coarse, the first tier that keeps data
// back to fromMs and has at most readFactor*maxPoints rows in the range
// (counted for the raw tier, estimated from the resolution for the
// others). If none keeps fromMs, the one that keeps the longest is used.
func (db *DB) chooseTier(id, fromMs, toMs int64, maxPoints int, tiers []Tier, nowMs int64) (chosenTier, error) {
	limit := int64(readFactor * maxPoints)
	longest := tiers[len(tiers)-1]
	for _, t := range tiers {
		covers := t.Keep == 0 || nowMs-t.Keep.Milliseconds() <= fromMs
		if !covers {
			continue
		}
		if t.Resolution == 0 {
			var n int64
			if err := db.sql.QueryRow(`SELECT count(*) FROM points WHERE series_id = ? AND t >= ? AND t < ?`, id, fromMs, toMs).Scan(&n); err != nil {
				return chosenTier{}, fmt.Errorf("store: %w", err)
			}
			if n <= limit || len(tiers) == 1 {
				return chosenTier{Tier: t, count: n}, nil
			}
			continue
		}
		if (toMs-fromMs)/t.Resolution.Milliseconds() <= limit || t == longest {
			return chosenTier{Tier: t}, nil
		}
	}
	return chosenTier{Tier: longest}, nil
}

// niceSteps are the widths points are merged to, in milliseconds.
var niceSteps = []int64{
	1_000, 2_000, 5_000, 10_000, 15_000, 30_000,
	60_000, 120_000, 300_000, 600_000, 900_000, 1_800_000,
	3_600_000, 7_200_000, 10_800_000, 21_600_000, 43_200_000,
	86_400_000, 2 * 86_400_000, 7 * 86_400_000, 14 * 86_400_000, 30 * 86_400_000,
}

// mergeStep returns the smallest nice width of at least res and at least
// span/maxPoints, rounded up to a whole multiple of res (so tier buckets
// are never split). Wider spans than the list covers double the last.
func mergeStep(res, span int64, maxPoints int) int64 {
	need := max(res, (span+int64(maxPoints)-1)/int64(maxPoints), 1)
	step := niceSteps[len(niceSteps)-1]
	for _, n := range niceSteps {
		if n >= need {
			step = n
			break
		}
	}
	for step < need {
		step *= 2
	}
	if res > 0 && step%res != 0 {
		step = (step/res + 1) * res
	}
	return step
}
