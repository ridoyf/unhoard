package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // registers the pure-Go "sqlite" driver
)

// Store is the app's SQLite database: the action log, scan snapshots and settings.
type Store struct {
	db *sql.DB
}

// migrations run in order; the database's user_version records how many have run.
var migrations = []string{
	`CREATE TABLE actions (
		id     INTEGER PRIMARY KEY,
		at     INTEGER NOT NULL,
		action TEXT NOT NULL,
		paths  TEXT NOT NULL,
		bytes  INTEGER NOT NULL,
		ok     INTEGER NOT NULL,
		result TEXT NOT NULL
	);
	CREATE TABLE snapshots (
		id        INTEGER PRIMARY KEY,
		at        INTEGER NOT NULL,
		root      TEXT NOT NULL,
		method    TEXT NOT NULL,
		bytes     INTEGER NOT NULL,
		files     INTEGER NOT NULL,
		vol_total INTEGER NOT NULL,
		vol_free  INTEGER NOT NULL
	);
	CREATE INDEX snapshots_root ON snapshots(root, at);
	CREATE TABLE snapshot_dirs (
		snapshot_id INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
		path        TEXT NOT NULL,
		bytes       INTEGER NOT NULL,
		files       INTEGER NOT NULL,
		PRIMARY KEY (snapshot_id, path)
	) WITHOUT ROWID;
	CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);`,
}

// Open opens (creating if needed) the database at path and applies migrations.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite allows one writer; one connection avoids "database is locked"
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		db.Close()
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	for i := version; i < len(migrations); i++ {
		if err := migrate(db, i); err != nil {
			db.Close()
			return nil, fmt.Errorf("migration %d: %w", i+1, err)
		}
	}
	return &Store{db: db}, nil
}

// migrate runs migration i and records it in one transaction, so a crash between the
// two cannot leave tables that the next start tries to create again.
func migrate(db *sql.DB, i int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(migrations[i]); err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Close() error { return s.db.Close() }

// Action is one entry in the action log.
type Action struct {
	ID     int64     `json:"id"`
	At     time.Time `json:"at"`
	Action string    `json:"action"`
	Paths  []string  `json:"paths"`
	Bytes  uint64    `json:"bytes"`
	OK     bool      `json:"ok"`
	Result string    `json:"result"`
}

// LogAction records something the app did (or tried to do).
func (s *Store) LogAction(a Action) error {
	if a.At.IsZero() {
		a.At = time.Now()
	}
	paths, _ := json.Marshal(a.Paths)
	_, err := s.db.Exec(`INSERT INTO actions (at, action, paths, bytes, ok, result) VALUES (?, ?, ?, ?, ?, ?)`,
		a.At.Unix(), a.Action, string(paths), a.Bytes, a.OK, a.Result)
	return err
}

// Actions returns the newest actions first.
func (s *Store) Actions(limit int) ([]Action, error) {
	rows, err := s.db.Query(`SELECT id, at, action, paths, bytes, ok, result FROM actions ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Action
	for rows.Next() {
		var a Action
		var at int64
		var paths string
		if err := rows.Scan(&a.ID, &at, &a.Action, &paths, &a.Bytes, &a.OK, &a.Result); err != nil {
			return nil, err
		}
		a.At = time.Unix(at, 0)
		json.Unmarshal([]byte(paths), &a.Paths)
		out = append(out, a)
	}
	return out, rows.Err()
}

// Snapshot summarises one completed scan.
type Snapshot struct {
	ID       int64     `json:"id"`
	At       time.Time `json:"at"`
	Root     string    `json:"root"`
	Method   string    `json:"method"`
	Bytes    uint64    `json:"bytes"`
	Files    uint64    `json:"files"`
	VolTotal uint64    `json:"volTotal"`
	VolFree  uint64    `json:"volFree"`
}

// DirSize is one folder's size in a snapshot.
type DirSize struct {
	Path  string `json:"path"`
	Bytes uint64 `json:"bytes"`
	Files uint64 `json:"files"`
}

// SaveSnapshot stores a snapshot and its folder sizes in one transaction.
func (s *Store) SaveSnapshot(snap Snapshot, dirs []DirSize) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO snapshots (at, root, method, bytes, files, vol_total, vol_free) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		snap.At.Unix(), snap.Root, snap.Method, snap.Bytes, snap.Files, snap.VolTotal, snap.VolFree)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO snapshot_dirs (snapshot_id, path, bytes, files) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()
	for _, d := range dirs {
		if _, err := stmt.Exec(id, d.Path, d.Bytes, d.Files); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}

// Snapshots returns snapshots of root, newest first. An empty root returns all.
func (s *Store) Snapshots(root string) ([]Snapshot, error) {
	rows, err := s.db.Query(`SELECT `+snapshotColumns+` FROM snapshots WHERE ? = '' OR root = ? ORDER BY at DESC`, root, root)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Snapshot
	for rows.Next() {
		sn, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sn)
	}
	return out, rows.Err()
}

// Snapshot returns one snapshot by id.
func (s *Store) Snapshot(id int64) (Snapshot, error) {
	return scanSnapshot(s.db.QueryRow(`SELECT `+snapshotColumns+` FROM snapshots WHERE id = ?`, id))
}

const snapshotColumns = `id, at, root, method, bytes, files, vol_total, vol_free`

// scanSnapshot reads snapshotColumns from a *sql.Row or *sql.Rows.
func scanSnapshot(row interface{ Scan(...any) error }) (Snapshot, error) {
	var sn Snapshot
	var at int64
	err := row.Scan(&sn.ID, &at, &sn.Root, &sn.Method, &sn.Bytes, &sn.Files, &sn.VolTotal, &sn.VolFree)
	sn.At = time.Unix(at, 0)
	return sn, err
}

// SnapshotDirs returns every stored folder size of a snapshot, keyed by path.
func (s *Store) SnapshotDirs(id int64) (map[string]DirSize, error) {
	rows, err := s.db.Query(`SELECT path, bytes, files FROM snapshot_dirs WHERE snapshot_id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]DirSize{}
	for rows.Next() {
		var d DirSize
		if err := rows.Scan(&d.Path, &d.Bytes, &d.Files); err != nil {
			return nil, err
		}
		out[d.Path] = d
	}
	return out, rows.Err()
}

// HistoryPoint is a folder's size at one snapshot.
type HistoryPoint struct {
	At    time.Time `json:"at"`
	Bytes uint64    `json:"bytes"`
}

// FolderHistory returns a folder's size across snapshots of root, oldest first.
// For the root itself it uses the volume's used space, which includes what the scan could not see.
func (s *Store) FolderHistory(root, path string) ([]HistoryPoint, error) {
	q := `SELECT s.at, d.bytes FROM snapshots s JOIN snapshot_dirs d ON d.snapshot_id = s.id
		WHERE s.root = ? AND d.path = ? ORDER BY s.at`
	args := []any{root, path}
	if path == root {
		q = `SELECT at, vol_total - vol_free FROM snapshots WHERE root = ? ORDER BY at`
		args = args[:1]
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HistoryPoint
	for rows.Next() {
		var p HistoryPoint
		var at int64
		if err := rows.Scan(&at, &p.Bytes); err != nil {
			return nil, err
		}
		p.At = time.Unix(at, 0)
		out = append(out, p)
	}
	return out, rows.Err()
}

// DeleteSnapshot removes a snapshot and its folder sizes.
func (s *Store) DeleteSnapshot(id int64) error {
	_, err := s.db.Exec(`DELETE FROM snapshots WHERE id = ?`, id)
	return err
}

// Setting returns a stored setting, or fallback if unset.
func (s *Store) Setting(key, fallback string) string {
	var v string
	if err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v); err != nil {
		return fallback
	}
	return v
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}
