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

## Time-of-day habits

A time-mode check-in records a wall-clock `time` ("23:40") alongside the usual
day. A period completes on one record, exactly like check mode; `targetTime`
adds punctuality on top: on-time rate, on-time/late counts and the mean recorded
time. The mean is **circular** (the vector mean on the 24-hour clock), so 23:50
and 00:10 average to 00:00 rather than to a same-day 12:00; samples spread evenly
around the clock have no meaningful mean, and the UI shows "—" there.

Late is decided on a **24-hour circle**: the gap from the deadline to the record,
wrapped into a day, counts as late only in the first ±12 hours. That is what
makes "asleep by 00:30" treat 23:40 as early and 00:40 as late, without asking
the user which side of midnight the deadline sits on. The credited `local_date`
still comes from the client, so a record logged after midnight belongs to the
new day; recording last night after the fact is what the backfill date is for.
`LateForDeadline` in Go and `isLate` in TypeScript implement the same rule and
carry the same table tests.

## Pinned, reminders and photos

`pinned` is one boolean on the project and the only stored ordering preference:
a pinned project leaves its category group and renders once at the top of Today.

Reminders are client-side on purpose (`reminderTime`): the server has no
scheduler and no push channel. While the app is open, a reminder is due for a
30-minute grace window after its time, never for a day already done/skipped/
rest, and once per project per local day (the fired keys persist in
localStorage). Browser notifications are opt-in from the settings page, because
permission prompts not tied to a user gesture are ignored by modern browsers.

A proof photo is one optional data URL on a check-in, capped at 400 KB. Bulk
reads (list, stats, overview, history) carry only `hasPhoto`; the bytes come from
`GET /checkins/{id}/photo`, which the browser caches as immutable. This keeps a
history page small no matter how many photos exist. Export/import still embed
the data URL so a backup is self-contained, which is why the import route has
its own, larger body cap.

## Login means everything

The whole service sits behind one administrator. Every data route — list,
detail, stats, check-ins, skips, categories, overview, export — answers `401`
without a session; only login/logout/me and the probes are public. There is no
per-project visibility flag. Login failures are uniform and rate-limited per
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
                    └──* Skip        clock time for time mode, optional photo,
                                     local_date + checked_at instant)
```

A `Project` carries one of six schedules; `target` means units per period
(day / ISO week / calendar month / N-day window), except `interval_hours`,
which has a rolling countdown instead of periods. It also carries the optional
`targetTime` (punctuality deadline), `reminderTime` (daily nudge) and `pinned`
(Today ordering). Extending the domain means: add the aggregate in
`internal/store`, its handlers in `internal/api`, and the mirror types in
`web/src/api/types.ts` — the two ends of the contract change together.
