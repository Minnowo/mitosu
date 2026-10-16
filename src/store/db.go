// The SQLite database that holds mitosu's history (docs/brief.md section
// 3, "Store"): opening it, its schema and its version.

package store

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

// schemaVersion is stored in PRAGMA user_version. A database with a newer
// version is refused; an older one is migrated by migrate.
const schemaVersion = 1

// schema creates version 1. Times are Unix milliseconds: the server's
// sample times, except events no sample dates (a store reset, a loss
// reported without samples), which use this machine's clock. hosts holds
// each host's cursor and what later samples are compared with: the last
// sample (for rates), the time of the last stored values, the last known
// boot ID, problem sources and container states (JSON, name to state).
// See docs/brief.md section 8 (M3) for the other tables.
const schema = `
CREATE TABLE hosts (
	id          INTEGER PRIMARY KEY,
	name        TEXT NOT NULL UNIQUE,
	store       TEXT NOT NULL DEFAULT '',
	next_seq    INTEGER NOT NULL DEFAULT 0,
	last_sample TEXT NOT NULL DEFAULT '',
	last_time   INTEGER NOT NULL DEFAULT -1,
	boot_id     TEXT NOT NULL DEFAULT '',
	problems    TEXT NOT NULL DEFAULT '',
	containers  TEXT NOT NULL DEFAULT '{}'
);
CREATE TABLE series (
	id      INTEGER PRIMARY KEY,
	host_id INTEGER NOT NULL REFERENCES hosts(id),
	kind    TEXT NOT NULL,
	entity  TEXT NOT NULL,
	metric  TEXT NOT NULL,
	UNIQUE (host_id, kind, entity, metric)
);
CREATE TABLE points (
	series_id INTEGER NOT NULL,
	t         INTEGER NOT NULL,
	v         NOT NULL,
	PRIMARY KEY (series_id, t)
) WITHOUT ROWID;
CREATE TABLE rollups (
	resolution INTEGER NOT NULL,
	series_id  INTEGER NOT NULL,
	t          INTEGER NOT NULL,
	n          INTEGER NOT NULL,
	sum        REAL NOT NULL,
	min        REAL NOT NULL,
	max        REAL NOT NULL,
	PRIMARY KEY (resolution, series_id, t)
) WITHOUT ROWID;
CREATE TABLE samples (
	host_id INTEGER NOT NULL,
	t       INTEGER NOT NULL,
	seq     INTEGER NOT NULL,
	PRIMARY KEY (host_id, t, seq)
) WITHOUT ROWID;
CREATE TABLE events (
	id       INTEGER PRIMARY KEY,
	host_id  INTEGER NOT NULL REFERENCES hosts(id),
	t        INTEGER NOT NULL,
	kind     TEXT NOT NULL,
	store    TEXT NOT NULL,
	seq_from INTEGER NOT NULL,
	seq_to   INTEGER NOT NULL,
	subject  TEXT NOT NULL,
	detail   TEXT NOT NULL,
	UNIQUE (host_id, kind, store, seq_from, seq_to, subject)
);
CREATE INDEX events_by_time ON events (host_id, t);
CREATE TABLE mitosu (
	marker TEXT NOT NULL
);
INSERT INTO mitosu (marker) VALUES ('mitosu history');
`

// DB is an open history database. It is safe for use by one process at a
// time; methods may be called from several goroutines.
type DB struct {
	sql *sql.DB

	// mu guards series, which caches series IDs by host, kind, entity
	// and metric.
	mu     sync.Mutex
	series map[seriesKey]int64
}

// seriesKey identifies a series.
type seriesKey struct {
	hostID               int64
	kind, entity, metric string
}

// DefaultPath returns $XDG_DATA_HOME/mitosu/mitosu.db, or
// ~/.local/share/mitosu/mitosu.db.
func DefaultPath() (string, error) {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" && filepath.IsAbs(dir) {
		return filepath.Join(dir, "mitosu", "mitosu.db"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "mitosu", "mitosu.db"), nil
}

// Open opens or creates the database at path. A new directory is made
// 0700 and a new file 0600, since the history names the user's servers
// and containers. It returns an error for a database written by a newer
// mitosu and for an SQLite file that is not mitosu's.
func Open(path string) (*DB, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	f, err := os.OpenFile(abs, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}

	conn, err := sql.Open("sqlite", sqliteURI(abs))
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	// One connection: SQLite has one writer, and this keeps pragmas and
	// transactions on the same connection.
	conn.SetMaxOpenConns(1)
	db := &DB{sql: conn, series: map[seriesKey]int64{}}
	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, err
	}
	return db, nil
}

// sqliteURI returns the SQLite URI for the absolute path abs, every path
// segment percent-encoded so that '%', '?' and '#' in names stay part of
// the file name. Transactions take the write lock at BEGIN (immediate),
// so two processes cannot both start writing.
func sqliteURI(abs string) string {
	parts := strings.Split(filepath.ToSlash(abs), "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	q := url.Values{}
	for _, p := range []string{"busy_timeout(5000)", "journal_mode(WAL)", "synchronous(NORMAL)", "foreign_keys(1)"} {
		q.Add("_pragma", p)
	}
	q.Set("_txlock", "immediate")
	return "file:" + strings.Join(parts, "/") + "?" + q.Encode()
}

// Close closes the database.
func (db *DB) Close() error {
	return db.sql.Close()
}

// migrate creates the schema in a new database, and refuses a newer
// one or an SQLite file that already holds other tables. The version is
// read again inside the transaction, so two first opens at once create
// the schema once.
func (db *DB) migrate() error {
	tx, err := db.sql.Begin()
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	defer rollback(tx)
	var version int
	if err := tx.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	switch {
	case version == schemaVersion:
		return nil
	case version > schemaVersion:
		return fmt.Errorf("store: database schema %d is newer than this mitosu (%d)", version, schemaVersion)
	case version != 0:
		return errors.New("store: unknown database schema")
	}
	var tables int
	if err := tx.QueryRow("SELECT count(*) FROM sqlite_schema").Scan(&tables); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	if tables != 0 {
		return errors.New("store: the file is an SQLite database that is not mitosu's")
	}
	if _, err := tx.Exec(schema); err != nil {
		return fmt.Errorf("store: create schema: %w", err)
	}
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	return tx.Commit()
}
