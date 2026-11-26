// Package store owns the domain: check-in projects, categories, check-ins,
// skips, the schedule engine that turns them into streaks and statistics, and
// the SQLite persistence behind all of it.
//
// The HTTP layer depends on the narrow interfaces declared here (CategoryStore,
// ProjectStore, CheckinStore, SkipStore, StatsStore), never on *DB, so a test
// can substitute any of them without touching a handler.
package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"uuid"

	// Pure-Go SQLite (no cgo): the release pipeline cross-compiles with
	// CGO_ENABLED=0 for six platforms, which a cgo driver would break.
	_ "modernc.org/sqlite"
)

// schemaSQL is applied when PRAGMA user_version is below schemaVersion.
//
//go:embed schema.sql
var schemaSQL string

const schemaVersion = 5

// migration2 adds the completion mode, the backfill marker and the
// prerequisite graph. Existing single-tick projects (target <= 1) become
// check mode; their behaviour is unchanged, the label just becomes honest.
const migration2SQL = `
ALTER TABLE projects ADD COLUMN mode TEXT NOT NULL DEFAULT 'value';
UPDATE projects SET mode = 'check' WHERE target <= 1;
ALTER TABLE checkins ADD COLUMN backfilled INTEGER NOT NULL DEFAULT 0;
CREATE TABLE IF NOT EXISTS prerequisites (
    project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    requires_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    PRIMARY KEY (project_id, requires_id)
);
`

// migration3 retires per-project visibility and adds the good/bad nature.
// The whole app now sits behind the administrator login, so the private flag
// has no meaning left; dropping it keeps the schema honest. Every existing
// project becomes a good habit (the default), which preserves its verdicts.
const migration3SQL = `
ALTER TABLE projects DROP COLUMN private;
ALTER TABLE projects ADD COLUMN nature TEXT NOT NULL DEFAULT 'good';
`

// migration4 adds the time-of-day mode and reminders: a punctuality deadline
// and a daily nudge on projects, and the recorded wall-clock time on check-ins.
// Existing rows default to empty (no deadline / no reminder / no time), so
// check and value habits are untouched.
const migration4SQL = `
ALTER TABLE projects ADD COLUMN target_time TEXT NOT NULL DEFAULT '';
ALTER TABLE projects ADD COLUMN reminder_time TEXT NOT NULL DEFAULT '';
ALTER TABLE checkins ADD COLUMN time TEXT NOT NULL DEFAULT '';
`

// migration5 adds pinning and proof photos. Pinned floats a project to the top
// of the Today page; photo is an optional image data URL on a check-in. Both
// default to off/empty, so existing rows keep their behaviour and size.
const migration5SQL = `
ALTER TABLE projects ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0;
ALTER TABLE checkins ADD COLUMN photo TEXT NOT NULL DEFAULT '';
`

// DB is the SQLite-backed store. One connection is deliberate: SQLite writes
// serialize anyway, and a single connection removes SQLITE_BUSY entirely while
// keeping every query in the microsecond range. WAL still lets the OS cache
// reads efficiently, and the HTTP layer never holds a transaction across a
// request boundary.
type DB struct {
	sql *sql.DB
	now func() time.Time
}

// Open opens (creating if needed) the database at path and migrates it.
// ":memory:" keeps everything in process memory, for tests. now supplies
// timestamps and may be nil, in which case time.Now is used.
func Open(path string, now func() time.Time) (*DB, error) {
	if now == nil {
		now = time.Now
	}

	dsn := path
	abs := ""
	if path != ":memory:" {
		var err error
		abs, err = filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("store: resolve db path: %w", err)
		}
		// The database is personal data: owner-only directory and files.
		if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
			return nil, fmt.Errorf("store: create db directory: %w", err)
		}
		dsn = filepath.ToSlash(abs)
	}

	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open sqlite: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(0)

	db := &DB{sql: sqlDB, now: now}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("store: ping sqlite: %w", err)
	}
	if err := db.configure(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	if err := db.migrate(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	if abs != "" {
		// Migration wrote the first pages, so the WAL sidecars exist by now.
		// Best effort: a filesystem that refuses chmod (some network mounts)
		// must not prevent the app from starting.
		for _, suffix := range []string{"", "-wal", "-shm"} {
			_ = os.Chmod(abs+suffix, 0o600)
		}
	}
	return db, nil
}

// configure applies the connection pragmas. With one connection they persist
// for the life of the process, so they are set here rather than in the DSN.
func (d *DB) configure(ctx context.Context) error {
	pragmas := []string{
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA foreign_keys = ON",
	}
	for _, p := range pragmas {
		if _, err := d.sql.ExecContext(ctx, p); err != nil {
			return fmt.Errorf("store: %s: %w", p, err)
		}
	}
	return nil
}

func (d *DB) migrate(ctx context.Context) error {
	var version int
	if err := d.sql.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("store: read schema version: %w", err)
	}
	if version >= schemaVersion {
		return nil
	}
	if version < 1 {
		if _, err := d.sql.ExecContext(ctx, schemaSQL); err != nil {
			return fmt.Errorf("store: apply schema v1: %w", err)
		}
	}
	if version < 2 {
		if _, err := d.sql.ExecContext(ctx, migration2SQL); err != nil {
			return fmt.Errorf("store: apply schema v2: %w", err)
		}
	}
	if version < 3 {
		if _, err := d.sql.ExecContext(ctx, migration3SQL); err != nil {
			return fmt.Errorf("store: apply schema v3: %w", err)
		}
	}
	if version < 4 {
		if _, err := d.sql.ExecContext(ctx, migration4SQL); err != nil {
			return fmt.Errorf("store: apply schema v4: %w", err)
		}
	}
	if version < 5 {
		if _, err := d.sql.ExecContext(ctx, migration5SQL); err != nil {
			return fmt.Errorf("store: apply schema v5: %w", err)
		}
	}
	if _, err := d.sql.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		return fmt.Errorf("store: stamp schema version: %w", err)
	}
	return nil
}

// Close closes the underlying connection pool.
func (d *DB) Close() error { return d.sql.Close() }

// Ping verifies the database is reachable; readiness probes use it.
func (d *DB) Ping(ctx context.Context) error { return d.sql.PingContext(ctx) }

// Today returns the server's calendar day, the default when a client sends none.
func (d *DB) Today() string { return DateKey(d.now()) }

// Categories returns the category store.
func (d *DB) Categories() *Categories { return &Categories{db: d} }

// Projects returns the project store.
func (d *DB) Projects() *Projects { return &Projects{db: d} }

// Checkins returns the check-in store.
func (d *DB) Checkins() *Checkins { return &Checkins{db: d} }

// Skips returns the skip store.
func (d *DB) Skips() *Skips { return &Skips{db: d} }

// Stats returns the statistics reader.
func (d *DB) Stats() *Stats { return &Stats{db: d} }

// isUniqueErr reports whether err is a SQLite UNIQUE constraint violation.
// The driver's error text is stable across versions for this case.
func isUniqueErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func toMillis(t time.Time) int64 { return t.UTC().UnixMilli() }

func fromMillis(ms int64) time.Time { return time.UnixMilli(ms).UTC() }

// categoryArg maps uuid.Nil() to SQL NULL so the foreign key stays happy.
func categoryArg(id uuid.UUID) any {
	if id == uuid.Nil() {
		return nil
	}
	return id.String()
}

func scanCategoryID(ns sql.NullString) uuid.UUID {
	if !ns.Valid || ns.String == "" {
		return uuid.Nil()
	}
	id, err := uuid.Parse(ns.String)
	if err != nil {
		return uuid.Nil()
	}
	return id
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// weekdaysCSV renders the canonical 1..7 list for storage.
func weekdaysCSV(days []int) string {
	parts := make([]string, len(days))
	for i, d := range days {
		parts[i] = fmt.Sprintf("%d", d)
	}
	return strings.Join(parts, ",")
}

func parseWeekdaysCSV(s string) []int {
	out := []int{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		var d int
		if _, err := fmt.Sscanf(part, "%d", &d); err == nil && d >= 1 && d <= 7 {
			out = append(out, d)
		}
	}
	return out
}

// likeEscape escapes LIKE wildcards so a search for "100%" matches literally.
func likeEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// errNoRows normalizes sql.ErrNoRows into the domain sentinel.
func errNoRows(err error, what string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrNotFound, what)
	}
	return err
}
