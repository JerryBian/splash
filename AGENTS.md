# AGENTS.md

Key points for AI agents (and contributors) working here. Read before changing
code. The invariants below are the things most easily broken by accident.

## What this is

`splash` — a mobile-first habit tracker. Go 1.27 backend + React 19 /
TypeScript 7 frontend, shipped as **one static binary**: the Vite build is copied
to `internal/web/dist` and baked in with `//go:embed all:dist`. SQLite on disk.

- **Language:** code/comments/docs/logs are English; the user-facing UI is
  Chinese. Server error codes stay English (a contract); `web/src/lib/errors.ts`
  is the only translation layer. Chinese literals live only in UI surfaces, demo
  seed data, the placeholder page, and UI default values.
- **One dependency:** `modernc.org/sqlite` (pure Go, so `CGO_ENABLED=0`
  cross-compiles). Everything else is stdlib. Justify any new dependency.
- **One role:** a single administrator (`SPLASH_ADMIN_*`). Every data route,
  reads included, requires login. No anonymous access, no per-project visibility.
- **Domain:** categories; projects with six schedule families, three completion
  modes (`check`/`value`/`time`) and a good/bad `nature`; check-ins (value, note,
  mood, a clock `time` for time mode, and an optional proof `photo`); skips; a
  cycle-free prerequisite graph; a **pure** schedule engine deriving streaks/
  misses/heatmaps/trends/violations on read. Time mode adds punctuality (on-time
  rate / on-time count / late count / average time) vs `targetTime`;
  `reminderTime` drives in-app reminders plus optional browser notifications;
  `pinned` floats a project to the top of Today; achievements are derived
  milestones (never stored).
- **Bad habits** (`nature=bad`): a check-in records a lapse; any occurrence spoils
  the period (`missed`), a clean ended period is `done`, the streak counts periods
  survived clean. Always check mode; `interval_hours` is rejected.

## Verify before declaring done

CI runs all six; run them and report real output (including failures):

```bash
gofmt -l .                  # must print nothing
go vet ./...
go test ./...               # -race needs cgo (Linux/macOS; not bare Windows)
pnpm -C web run typecheck   # tsc --noEmit, TS7 native
pnpm -C web run lint        # oxlint (NOT eslint)
pnpm -C web test
```

Build/dev: `node scripts/build.mjs` (frontend+embed+binary → `bin/`);
`node scripts/dev.mjs` (Go :8080 + Vite :5173); `go run ./cmd/server` (backend
only). Windows one-click: `pwsh -File scripts/run.ps1`. `make` is optional.

## Architecture

`cmd → server → {api, web} → {store, auth, httpx} → config/version` (one-way).
`httpx` never imports `store`; only `internal/api/errors.go` maps domain sentinels
to HTTP status.

- `internal/httpx` — transport: JSON (`encoding/json/v2`), error envelope,
  middleware (RequestID, Logging, Recovery, SecurityHeaders, CORS, NoCache).
- `internal/auth` — constant-time credential check, HMAC session tokens,
  cookie/bearer middleware, per-IP login limiter.
- `internal/store` — domain + SQLite. `domain.go` (types/validation),
  `schedule.go` (pure `BuildStats`), `db.go` (schema/migrations), one file per
  aggregate, `stats.go`, `seed.go`, `import.go`. API depends on narrow interfaces.
- `internal/api` — thin handlers: parse → store → encode. `api.fail` is the only
  error exit; `api.guard` wraps every data route with `requireAdmin`.
- `internal/web` — embed + SPA fallback + placeholder. `internal/server` —
  assembly, middleware order, graceful shutdown.
- `web/src` — `api/client.ts` is the only `fetch` caller; `lib/async.ts` caches
  `use()`-based loads.

## Hard invariants

**Go**
- Errors are values; wrap sentinels (`%w`). Domain returns `ErrNotFound` /
  `ErrConflict` / `ErrVersionConflict` / `ValidationErrors`; only `api` maps them.
- Validation reports all fields at once (`ValidationErrors`, `Is`→`ErrValidation`).
- Optimistic concurrency lives in SQL: `UPDATE ... WHERE id=? AND version=?`.
  Never check-then-write. `If-Match` → 412 via `ErrVersionConflict`.
- Access enforced once at the edge: register data routes through `a.guard(...)`.
  Only `auth/login|logout|me` and `health|ready|version` are ungated.
- `BuildStats` is pure (no clock, no queries). New schedule rules go there with
  table-driven tests. The bad-habit inversion lives there too.
- Table-driven tests; `t.Helper()`; `t.Setenv`; injected clocks;
  `httptest.NewTestServer(t, h)`; API tests use a real `store.Open(":memory:")`.
- Benchmarks: `for b.Loop()`. Every exported identifier has a doc comment;
  comments explain *why*.

**TypeScript / React**
- `strict`, `noUncheckedIndexedAccess`, `verbatimModuleSyntax`,
  `erasableSyntaxOnly` (no enums/namespaces/parameter properties).
- Use React 19 deliberately: `use()`+Suspense, `useOptimistic`, `useDeferredValue`,
  `useTransition` — not `useEffect`-and-state. Promises for `use()` are created in
  a `useState` initializer / key-only `useMemo`, stay stable, and never reject
  (failures become discriminated results). Suspending tests render inside awaited
  `act(async () => ...)`.
- `api/types.ts` mirrors the Go structs by hand — **change both ends together**.
- Closed validated ink palette (`INK_COLORS`); color follows the entity and always
  ships with an icon/name; status uses shapes (dashed/×) too; text wears ink
  tokens, never series color. Chrome wears celadon `--brand`; red is errors only.
- **One DOM, two shells** at the 1024px breakpoint (phone bottom-nav/sheets vs
  desktop rail/columns), purely via media queries. Both navs exist in the DOM →
  tests use `getAllByText`. Detail/stats column wrappers use `display:contents` +
  `order` on mobile — don't reorder that JSX.

## Gotchas

- `internal/web/dist/.gitkeep` must stay committed or `go:embed` fails on a fresh
  clone. `web/dist` and `internal/web/dist/*` are build output, never committed.
- `.dockerignore` excludes `internal/web/dist/*` (the Dockerfile `COPY` merges, so
  a stale host build would leak old bundles).
- Don't add `typescript-eslint` (peer range excludes TS7). Don't drop `-race` from
  CI to "fix" Windows.
- The `/api/` catch-all matters: without it unknown API paths fall through to the
  SPA and answer `200` HTML. `handleUnmatched` also yields `405`+`Allow`.
- IDs are UUIDv7 (time-ordered) on purpose.
- **One SQLite connection: a tx owns the whole pool.** Never call a pool-backed
  store method inside an open `BeginTx` (deadlock). `SeedDemo`/`Import` do pool
  reads first, then raw-SQL inserts in the tx.
- `local_date` is the client's day; `checked_at` is the instant. Periods/streaks/
  heatmaps use `local_date`; only interval-hours countdown + history ordering use
  `checked_at`. A backfill sets `local_date` to the requested day, `checked_at` to
  noon UTC of it. Don't "fix" one to match the other.
- Punctuality compares clocks on a **24-hour circle** (`LateForDeadline` in Go,
  `isLate` in TS): a deadline at 00:30 treats 23:40 as early and 00:40 as late, so
  never replace it with a lexicographic `"HH:MM"` string compare.
- Proof photos are data URLs capped at `MaxPhotoBytes` (400 KB). They are **never**
  embedded in list/stats responses — those carry `hasPhoto`, and
  `GET /checkins/{id}/photo` streams the bytes (immutable, private cache). Export
  and import still carry the data URL, which is why `/import` uses the 32 MiB
  configuration ceiling instead of the configured default body limit.
- Pinned projects leave their category group and render once in Today's 置顶
  section. Don't render them in both places.
- Statuses are derived, never stored (only `checkins.backfilled` is stored). A
  backfill/undo repairs history retroactively; no migration needed for schedule
  rules.
- `schema.sql` is the frozen v1 baseline. Schema changes go in a new
  `migrationNSQL` const + `schemaVersion` bump in `db.migrate` — never add columns
  to `schema.sql` (fresh DBs run migrations too → collision).
- Prerequisite cycles/self/dangling edges rejected in `checkPrereqGraph`. Ids are
  returned as-is (one admin, no anonymous stripping).
- Session cookie is browser-scoped (no MaxAge); token expiry is an idle timeout
  slid by `auth.Middleware` + `useSessionHeartbeat`. Don't add MaxAge or make
  expiry absolute. Production defaults `CookieSecure=true`; HSTS is
  production-only. Login limiter resets on a successful sign-in.
- Default credentials warn at startup (`UsingDefaultCredentials`); keep it wired.
- `api.fail` is the only handler error exit (keeps logging + 5xx redaction).
  `httpx.WriteJSON` buffers before writing — don't make it streaming.
- `DecodeJSON` requires `application/json` (CSRF hardening) → 415 otherwise.
- CORS `*` ignores `AllowCredentials` (deliberate). Tab icon is `/icon.svg` (not
  favicon.svg); regenerate PNG companions if the mark changes.
- Windows containers are a separate `splash-windows` image; Linux stays
  distroless. Both tag only `latest` + exact version. `build.mjs` maps
  `win32→windows`, `x64→amd64`.

## Extending the domain

1. `store/domain.go` — aggregate + input/patch + validation (keep
   `ValidationErrors` shape).
2. `store/<aggregate>.go` — narrow interface + SQLite impl; expose from `DB`, add
   to `api.Deps`. Migrations per the gotcha above.
3. `internal/api` — routes via `a.guard`, request structs, DTO converters.
4. `web/src/api/types.ts` + `client.ts` — same commit.
5. Schedule rules in `store/schedule.go` (pure, table-tested); frontend reads
   verdicts via `lib/schedule.ts`.
6. Tests: `store/*_test.go`, `api/api_test.go` (+ `security_test.go`),
   `web/src/**/*.test.ts(x)`.

## Working agreements

- **Don't commit or push unless asked.** Leave changes in the tree; describe them.
- Never commit build output, coverage, `.env`, `data/`, or credentials.
- `pnpm-lock.yaml` changes ship with `web/package.json` (CI uses
  `--frozen-lockfile`).
- API shape changes update `types.ts` in the same change; mention it.
- Comments/docs explain reasoning, not mechanics.
- Release notes are generated from the commits since the previous tag
  (`release.yml`) — don't hardcode a repeating description.
