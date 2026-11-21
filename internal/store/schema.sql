-- splash schema, version 1.
--
-- Timestamps are INTEGER milliseconds UTC; calendar days are TEXT YYYY-MM-DD
-- in the client's timezone, because every period computation is about the
-- user's day, not the server's. UUIDv7 primary keys are time-ordered, so
-- index appends stay sequential.

CREATE TABLE IF NOT EXISTS categories (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    icon       TEXT NOT NULL DEFAULT '',
    color      TEXT NOT NULL DEFAULT 'blue',
    created_at INTEGER NOT NULL
);

-- Case-insensitive uniqueness: "健康" and "健康 " are caught by validation,
-- but "Health" vs "health" is the database's job.
CREATE UNIQUE INDEX IF NOT EXISTS idx_categories_name ON categories(name COLLATE NOCASE);

CREATE TABLE IF NOT EXISTS projects (
    id             TEXT PRIMARY KEY,
    name           TEXT NOT NULL,
    description    TEXT NOT NULL DEFAULT '',
    icon           TEXT NOT NULL DEFAULT '✅',
    color          TEXT NOT NULL DEFAULT 'blue',
    category_id    TEXT REFERENCES categories(id) ON DELETE SET NULL,
    private        INTEGER NOT NULL DEFAULT 0,
    schedule       TEXT NOT NULL,
    target         REAL NOT NULL DEFAULT 1,
    unit           TEXT NOT NULL DEFAULT '次',
    weekdays       TEXT NOT NULL DEFAULT '',
    interval_value REAL NOT NULL DEFAULT 0,
    start_date     TEXT NOT NULL,
    archived       INTEGER NOT NULL DEFAULT 0,
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL,
    version        INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_projects_category ON projects(category_id);

CREATE TABLE IF NOT EXISTS checkins (
    id         TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    value      REAL NOT NULL DEFAULT 1,
    note       TEXT NOT NULL DEFAULT '',
    mood       TEXT NOT NULL DEFAULT '',
    local_date TEXT NOT NULL,
    checked_at INTEGER NOT NULL,
    tz_offset  INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL
);

-- The two access patterns that matter: day aggregates for stats, and instant
-- ordering for interval_hours countdowns and history.
CREATE INDEX IF NOT EXISTS idx_checkins_project_date ON checkins(project_id, local_date);
CREATE INDEX IF NOT EXISTS idx_checkins_project_time ON checkins(project_id, checked_at);

CREATE TABLE IF NOT EXISTS skips (
    id         TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    skip_date  TEXT NOT NULL,
    reason     TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    UNIQUE(project_id, skip_date)
);
