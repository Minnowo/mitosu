// Records when pulling a host starts failing and when it works again, so
// the history shows when mitosu could not reach a server.

package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// RecordPullStatus records a change of host's pull status at t (Unix
// milliseconds of this machine): EventPullFailed with detail when failed,
// else EventPullOK. The caller records changes only, not every pull.
// The time is also the event's sequence range, which keeps two changes
// at different times distinct.
func (db *DB) RecordPullStatus(host string, t int64, failed bool, detail string) error {
	if t < 0 {
		return fmt.Errorf("store: negative time %d", t)
	}
	kind := EventPullOK
	if failed {
		kind = EventPullFailed
	}
	tx, err := db.sql.Begin()
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	defer rollback(tx)
	hostID, err := hostRow(tx, host)
	if err != nil {
		return err
	}
	if err := addEvent(tx, hostID, uint64(t), kind, "", uint64(t), uint64(t), "", detail); err != nil {
		return err
	}
	return tx.Commit()
}

// PullFailing reports whether host's last recorded pull status is
// EventPullFailed (false for a host with none), so a restarted serve
// knows to record EventPullOK when pulling works again.
func (db *DB) PullFailing(host string) (bool, error) {
	var kind string
	err := db.sql.QueryRow(`SELECT e.kind FROM events e JOIN hosts h ON h.id = e.host_id
		WHERE h.name = ? AND e.kind IN (?, ?) ORDER BY e.t DESC, e.id DESC LIMIT 1`,
		host, EventPullFailed, EventPullOK).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("store: %w", err)
	}
	return kind == EventPullFailed, nil
}
