// Reads recorded events back (what ingest.go records).

package store

import "fmt"

// Event is one recorded event of a host.
type Event struct {
	// TimeMs is when it happened (see the schema comment in db.go for
	// which clock).
	TimeMs int64
	// Kind is one of the Event* constants.
	Kind  string
	Store string
	// SeqFrom and SeqTo are the sample numbers it concerns.
	SeqFrom, SeqTo int64
	// Subject is the container name for container events, else "".
	Subject string
	// Detail is text for humans; it comes from the server and may hold
	// any characters.
	Detail string
}

// Events returns at most limit of host's events with fromMs <= TimeMs <
// toMs: the newest ones, oldest first. A host never pulled has none.
func (db *DB) Events(host string, fromMs, toMs int64, limit int) ([]Event, error) {
	if limit < 1 {
		return nil, fmt.Errorf("store: event limit %d", limit)
	}
	rows, err := db.sql.Query(`SELECT t, kind, store, seq_from, seq_to, subject, detail FROM (
		SELECT e.id, e.t, e.kind, e.store, e.seq_from, e.seq_to, e.subject, e.detail
		FROM events e JOIN hosts h ON h.id = e.host_id
		WHERE h.name = ? AND e.t >= ? AND e.t < ? ORDER BY e.t DESC, e.id DESC LIMIT ?
	) ORDER BY t, id`, host, fromMs, toMs, limit)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.TimeMs, &e.Kind, &e.Store, &e.SeqFrom, &e.SeqTo, &e.Subject, &e.Detail); err != nil {
			return nil, fmt.Errorf("store: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	return out, nil
}
