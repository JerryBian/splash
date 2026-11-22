# Design notes

This page is for contributors who want the reasoning behind the implementation.
[AGENTS.md](../AGENTS.md) is the practical guide: architecture, invariants and
the commands to run before submitting a change.

## One binary, one dependency

The Go module has exactly one third-party dependency: `modernc.org/sqlite`, the
pure-Go SQLite driver. The router, JSON, UUIDs, logging, validation and session
tokens are standard library. Keeping the SQLite driver pure Go is what makes the
`CGO_ENABLED=0` cross-platform release — and the distroless container — possible
without a C toolchain.

The frontend is embedded with `go:embed`, so there is one artifact to deploy and
no way for the UI and API to drift apart.

## SQLite with one connection

Writes serialize in SQLite anyway; a single connection removes `SQLITE_BUSY`
while every query stays in the microsecond range. WAL, `synchronous=NORMAL` and
covering indexes on `(project_id, local_date)` and `(project_id, checked_at)`
carry the statistics. The corollary: never run a pool-backed query inside an
open transaction — the transaction holds the only connection the pool has.

## Statuses are derived, never stored

Streaks, misses, heatmaps and prerequisite violations are computed from
check-ins + skips + the schedule + the prerequisite graph on every read
(`store.BuildStats` / `store.PrereqSatisfied`, both pure). There is no cron job
to drift and no status column to repair: backfilling a day repairs a streak
retroactively, and backfilling a prerequisite clears the dependent project's
violation for that day.

## The client's day is the day

Periods, streaks and heatmaps use the `local_date` the client sends (its own
timezone), not the server's clock. Instants (`checked_at`) drive only the
interval-hours countdown and history ordering. A backfill sets `local_date` to
the requested day and `checked_at` to noon UTC of that day.

## Private means invisible

Anonymous reads of a private project answer `404` on every path — list, detail,
stats, check-ins, skips — so existence is not leaked. Writes answer `401`.
Login failures are uniform and rate-limited per IP; passwords are compared
constant-time.

## The UI shell

One DOM, two shells. Below 1024px the app is the phone layout (bottom navigation,
bottom sheets); at 1024px+ a sidebar rail, multi-column canvases and centered
dialogs take over, purely through media queries. The ink palette is closed and
validated; every colored mark also carries an icon or a name, and heatmap
statuses use shapes (dashed = skipped, × = missed) as well as color.

## Domain model

```
Category 1──* Project 1──* Checkin   (value in the project's unit, note, mood,
                    └──* Skip        local_date + checked_at instant)
```

A `Project` carries one of six schedules; `target` means units per period
(day / ISO week / calendar month / N-day window), except `interval_hours`,
which has a rolling countdown instead of periods. Extending the domain means:
add the aggregate in `internal/store`, its handlers in `internal/api`, and the
mirror types in `web/src/api/types.ts` — the two ends of the contract change
together.
