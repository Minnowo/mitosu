// Deletes values older than their tier keeps them (docs/brief.md
// section 3, "Tiers").

package store

import (
	"fmt"
	"time"
)

// Prune deletes, at time now, raw points and rollup buckets older than
// their tier's Keep, sample records older than the first tier's Keep, and
// events older than the longest Keep (none if a tier is kept forever).
// Rollups of resolutions not in tiers are left alone. It returns the
// number of rows deleted.
func (db *DB) Prune(tiers []Tier, now time.Time) (int64, error) {
	if err := ValidateTiers(tiers); err != nil {
		return 0, err
	}
	nowMs := now.UnixMilli()
	var deleted int64
	exec := func(query string, args ...any) error {
		res, err := db.sql.Exec(query, args...)
		if err != nil {
			return fmt.Errorf("store: prune: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("store: prune: %w", err)
		}
		deleted += n
		return nil
	}

	longest := time.Duration(0)
	forever := false
	for _, t := range tiers {
		if t.Keep == 0 {
			forever = true
		} else if t.Keep > longest {
			longest = t.Keep
		}
		if t.Keep == 0 {
			continue
		}
		cutoff := nowMs - t.Keep.Milliseconds()
		if t.Resolution == 0 {
			// By series, so the (series_id, t) key is used.
			if err := exec("DELETE FROM points WHERE series_id IN (SELECT id FROM series) AND t < ?", cutoff); err != nil {
				return deleted, err
			}
			continue
		}
		// A bucket goes when all of it is older than the cutoff. By
		// series, so the (resolution, series_id, t) key is used.
		res := t.Resolution.Milliseconds()
		if err := exec("DELETE FROM rollups WHERE resolution = ? AND series_id IN (SELECT id FROM series) AND t <= ?", res, cutoff-res); err != nil {
			return deleted, err
		}
	}
	if first := tiers[0]; first.Keep != 0 {
		if err := exec("DELETE FROM samples WHERE host_id IN (SELECT id FROM hosts) AND t < ?", nowMs-first.Keep.Milliseconds()); err != nil {
			return deleted, err
		}
	}
	if !forever {
		if err := exec("DELETE FROM events WHERE host_id IN (SELECT id FROM hosts) AND t < ?", nowMs-longest.Milliseconds()); err != nil {
			return deleted, err
		}
	}
	return deleted, nil
}
