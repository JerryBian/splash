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

Streaks, misses, heatmaps, prerequisite violations, postrequisite commitments
and their broken history are computed from check-ins + skips + the schedule +
the link graph on every read (`store.BuildStats` / `store.PrereqSatisfied`, both
pure). There is no cron job to drift and no status column to repair: backfilling
a day repairs a streak retroactively, backfilling a prerequisite clears the
dependent project's violation for that day, and backfilling a postrequisite
clears `未兑现` for the day it was owed.

## The client's day is the day

Periods, streaks and heatmaps use the `local_date` the client sends (its own
timezone), not the server's clock. Instants (`checked_at`) drive only the
interval-hours countdown and history ordering. A backfill sets `local_date` to
the requested day and `checked_at` to noon UTC of that day.

## Supplement notes

Every check-in carries an optional free-text `note`. A project may set a
`notePrompt` — "什么时候睡的？睡了多久？" — which becomes the note's heading in
the record sheet; the note itself is written per check-in and shown verbatim in
the history, the day panel and the sheet's day list. This replaced the earlier
clock-time mode: when migration 7 removed `mode=time`, any recorded clock values
were appended to the note and the old deadline became the starting prompt, so
the useful data survives without a special-case field.

## Same-day links

Prerequisites and postrequisites are **one directed graph**, stored as
`project_links(source_id, target_id, kind)`:

- `requires` — a prerequisite: the target must be resolved on a day the source
  is checked. If it is not, the check-in carries a prerequisite violation.
- `implies` — a postrequisite: a completed source commits the target to the same
  day. If the target is not resolved, the source carries an unfulfilled
  commitment.

Both directions are same-day and share one cycle rule: a target can never reach
its source through the union of both kinds. A pair cannot be both a prerequisite
and a postrequisite of the same project, and a bad habit cannot have
postrequisites because abstention has no completion event.

A postrequisite fires on the source's **completion day** — the first day its
period target was reached (for a weekly goal of three, the day of the third
check-in, not every later day of the week). For `interval_hours` there is no
period target, so every day with a check-in is its own event and commits that
day. Targets resolve under the same rule as prerequisites: archived, skipped and
not-scheduled projects count as resolved; a good target needs its period target
by that day, a bad target needs a clean day. Today's completion is `pending`
(still fulfillable); once the day closes, an unresolved target becomes `broken`
and stays in the lifetime `BrokenCommitments` until the target is backfilled onto
that same day.

The source's own `done`/`missed` status is **not rewritten** by a broken
commitment: the run still happened, and erasing it would make the calendar lie.
The broken promise is a separate derived violation with its own counts, list and
completion rate, so the pair is enforced without conflating fact and promise.

## Destructive reset

`POST /admin/reset` is the one operation that cannot run alongside anything else.
A valid session is not enough: the administrator re-enters the configured
password, checked constant-time, so a borrowed browser cannot erase the data.
The handler takes an exclusive `dbMu`; every data route holds its read side
(and readiness too), so in-flight requests finish first and new ones wait. It
then closes the SQLite pool, removes the database and its `-wal`/`-shm`/
`-journal` sidecars, reopens the same path, reapplies pragmas and migrations,
and re-seeds demo data only when `SPLASH_SEED_DEMO` is on. The session lives in
config-signed cookies, not SQLite, so it survives; the client reloads to drop
every cached list. If deletion fails, the store reopens the old file rather than
leaving the process with a closed pool.

## Pinned and photos

`pinned` is one boolean on the project and the only stored ordering preference:
a pinned project leaves its category group and renders once at the top of Today.

A proof photo is one optional data URL on a check-in, capped at 400 KB. Bulk
reads (list, stats, overview, history) carry only `hasPhoto`; the bytes come from
`GET /checkins/{id}/photo`, which the browser caches as immutable. This keeps a
history page small no matter how many photos exist. Export/import still embed
the data URL so a backup is self-contained, which is why the import route has
its own, larger body cap.

## Login means everything

The whole service sits behind one administrator. Every data route — list,
detail, stats, check-ins, skips, categories, overview, export, reset — answers
`401` without a session; only login/logout/me and the probes are public. There is
no per-project visibility flag. Login failures are uniform and rate-limited per
IP; passwords are compared constant-time.

## Good habits and bad habits

A project's `nature` is `good` (build it) or `bad` (break it). The schedule
engine inverts the verdicts for a bad habit: a check-in records a lapse, any
occurrence spoils the period (`missed`), a clean ended period is the success
(`done`), and the streak counts periods survived without a lapse. The overview
tallies good-habit check-ins and bad-habit lapses (slips) separately so a lapse
never inflates an achievement number. Bad habits are always check mode and
cannot use `interval_hours`.

## The UI shell

One DOM, two shells. Below 1024px the app is the phone layout (bottom navigation,
bottom sheets); at 1024px+ a sidebar rail, multi-column canvases and centered
dialogs take over, purely through media queries. The ink palette is closed and
validated; every colored mark also carries an icon or a name, and heatmap
statuses use shapes (dashed = skipped, × = missed) as well as color.

## Domain model

```
Category 1──* Project 1──* Checkin   (value in the project's unit, note, mood,
                    └──* Skip        optional photo, local_date + checked_at)
             Project *──* Project    (same-day links: requires / implies)
```

A `Project` carries one of six schedules; `target` means units per period
(day / ISO week / calendar month / N-day window), except `interval_hours`,
which has a rolling countdown instead of periods. It also carries the optional
`notePrompt` (a heading for check-in supplements), `pinned` (Today ordering) and
two same-day link lists (`prerequisites` / `postrequisites`). Extending the
domain means: add the aggregate in `internal/store`, its handlers in
`internal/api`, and the mirror types in `web/src/api/types.ts` — the two ends
of the contract change together.
