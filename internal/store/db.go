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

const schemaVersion = 7

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

// migration6 unifies the same-day dependency graph into project_links: the old
// prerequisites table becomes 'requires' edges, and postrequisites are
// 'implies' edges in the same table. One graph means one cycle rule, one
// loader, and one place to keep both directions consistent.
const migration6SQL = `
CREATE TABLE IF NOT EXISTS project_links (
    source_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    target_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    kind      TEXT NOT NULL CHECK (kind IN ('requires', 'implies')),
    PRIMARY KEY (source_id, target_id)
);
INSERT INTO project_links (source_id, target_id, kind)
    SELECT project_id, requires_id, 'requires' FROM prerequisites;
DROP TABLE prerequisites;
CREATE INDEX IF NOT EXISTS idx_project_links_target ON project_links(target_id, kind);
`

// migration7 replaces the failed clock-mode experiment with a per-project
// supplement prompt: the recorded clock times move into the free-text note,
// old "time" projects become ordinary check projects, the deadline survives as
// the prompt's starting text, and the unused time/reminder columns are dropped.
const migration7SQL = `
ALTER TABLE projects ADD COLUMN note_prompt TEXT NOT NULL DEFAULT '';
UPDATE checkins
   SET note = CASE
       WHEN note = '' THEN time
       ELSE note || ' · ' || time
   END
 WHERE time <> '';
UPDATE projects SET note_prompt = '原准时目标 ' || target_time WHERE target_time <> '';
UPDATE projects SET mode = 'check' WHERE mode = 'time';
ALTER TABLE projects DROP COLUMN target_time;
ALTER TABLE projects DROP COLUMN reminder_time;
ALTER TABLE checkins DROP COLUMN time;
`

// DB is the SQLite-backed store. One connection is deliberate: SQLite writes
// serialize anyway, and a single connection removes SQLITE_BUSY entirely while
// keeping every query in the microsecond range. WAL still lets the OS cache
// reads efficiently, and the HTTP layer never holds a transaction across a
// request boundary.
type DB struct {
	sql *sql.DB
	now func() time.Time
	// dsn and abs are kept so Reset can reopen the same database after deleting
	// its files. abs is empty for the in-memory test database.
	dsn string
	abs string
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

	sqlDB, err := openSQLite(dsn)
	if err != nil {
		return nil, err
	}

	db := &DB{sql: sqlDB, now: now, dsn: dsn, abs: abs}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.prepare(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return db, nil
}

// openSQLite opens the pool with the single-connection policy the store relies
// on. Keeping it separate lets Reset recreate the pool for the same DSN.
func openSQLite(dsn string) (*sql.DB, error) {
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open sqlite: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(0)
	return sqlDB, nil
}

// prepare pings, applies the connection pragmas and migrates the schema. The
// caller owns the pool and closes it if prepare fails.
func (d *DB) prepare(ctx context.Context) error {
	if err := d.sql.PingContext(ctx); err != nil {
		return fmt.Errorf("store: ping sqlite: %w", err)
	}
	if err := d.configure(ctx); err != nil {
		return err
	}
	if err := d.migrate(ctx); err != nil {
		return err
	}
	d.chmod()
	return nil
}

// chmod makes the database and its sidecars owner-only. Best effort: a
// filesystem that refuses chmod (some network mounts) must not prevent startup.
func (d *DB) chmod() {
	if d.abs == "" {
		return
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		_ = os.Chmod(d.abs+suffix, 0o600)
	}
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
	if version < 6 {
		if _, err := d.sql.ExecContext(ctx, migration6SQL); err != nil {
			return fmt.Errorf("store: apply schema v6: %w", err)
		}
	}
	if version < 7 {
		if _, err := d.sql.ExecContext(ctx, migration7SQL); err != nil {
			return fmt.Errorf("store: apply schema v7: %w", err)
		}
	}
	if _, err := d.sql.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		return fmt.Errorf("store: stamp schema version: %w", err)
	}
	return nil
}

// Close closes the underlying connection pool.
func (d *DB) Close() error { return d.sql.Close() }

// Reset deletes the database files and recreates the schema, optionally seeding
// the demo data. It is destructive and must be serialized against every other
// query: the HTTP layer holds an exclusive lock for the whole call, so no
// request can observe the pool between Close and the fresh open.
//
// The request context is detached from cancellation: once the delete has begun,
// stopping halfway because the browser hung up would leave a half-reset app.
func (d *DB) Reset(ctx context.Context, seed bool) (int, error) {
	resetCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()

	if err := d.sql.Close(); err != nil {
		return 0, fmt.Errorf("store: close for reset: %w", err)
	}
	if d.abs != "" {
		// Remove the sidecars first: if the main file cannot be removed, reopening
		// the old database is safer without a stale WAL next to it.
		for _, suffix := range []string{"-wal", "-shm", "-journal", ""} {
			if err := os.Remove(d.abs + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
				// Reopen what is still there: a failed delete must not leave every
				// later request talking to a closed pool.
				if reopenErr := d.reopen(resetCtx); reopenErr != nil {
					return 0, fmt.Errorf("store: remove database file: %v (reopen: %w)", err, reopenErr)
				}
				return 0, fmt.Errorf("store: remove database file: %w", err)
			}
		}
	}
	if err := d.reopen(resetCtx); err != nil {
		return 0, err
	}
	if !seed {
		return 0, nil
	}
	seeded, err := d.SeedDemo(resetCtx)
	if err != nil {
		return 0, fmt.Errorf("store: seed after reset: %w", err)
	}
	d.chmod()
	return seeded, nil
}

// reopen replaces the closed pool with a fresh one for the same DSN.
func (d *DB) reopen(ctx context.Context) error {
	sqlDB, err := openSQLite(d.dsn)
	if err != nil {
		return err
	}
	d.sql = sqlDB
	if err := d.prepare(ctx); err != nil {
		_ = sqlDB.Close()
		return err
	}
	return nil
}

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
