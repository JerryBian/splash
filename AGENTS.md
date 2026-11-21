# AGENTS.md

Instructions for AI coding agents (and new contributors) working in this
repository. Read this before changing code; the invariants at the bottom are the
part most likely to be broken by accident.

## What this project is

`splash` is a mobile-first check-in habit tracker: a Go 1.27 backend and a
React 19 + TypeScript 7 frontend shipped as **one static binary** with SQLite
on disk. The Vite build is copied into `internal/web/dist` and baked in with
`//go:embed all:dist`. Code, comments, docs, workflows and logs are English;
the user-facing UI is Chinese. Server error codes stay English as a contract
and `web/src/lib/errors.ts` is the single translation layer. Chinese literals
are confined to UI surfaces: `web/src` UI strings and their tests, the demo
seed data, the built-frontend placeholder page, and UI default values (the
default unit and the uncategorized bucket label) with their assertions.

The domain is real: categories, projects with six schedule families and two
completion modes (`check` = one tick, `value` = accumulate), check-ins (value +
note + mood + a stored backfill flag), skips, a prerequisite graph (same-day
gates, AND, cycle-free), and a pure schedule engine that derives streaks,
misses, heatmaps, trends and prerequisite violations on read. Two roles:
anonymous visitors read public projects; one administrator (credentials from
`SPLASH_ADMIN_*`) writes
everything and sees private projects.

The Go module has **one third-party dependency**: `modernc.org/sqlite`, the
pure-Go SQLite driver. It is there because SQLite is a product requirement and
cgo would break the `CGO_ENABLED=0` six-platform release. Everything else —
router, JSON, UUIDs, logging, validation, auth (HMAC session tokens) — is
standard library. Justify any new dependency in the PR description.

## Commands

Go and Node are the only prerequisites. `make` is optional — on Windows it is
usually absent, and every target is a thin wrapper over `node scripts/build.mjs`
or a plain `go`/`pnpm` command.

```bash
# --- setup ---
go mod download
pnpm -C web install --frozen-lockfile     # npm -C web ci also works

# --- develop (both processes, prefixed output, Ctrl+C stops both) ---
node scripts/dev.mjs                      # Go on :8080, Vite on :5173

# --- Windows one-click: build + run + fresh demo data + open browser ---
pwsh -File scripts/run.ps1                # -SkipBuild / -KeepData / -Port / -Foreground
                                          # demo data goes to data/demo.db, never data/splash.db

# --- backend only, no frontend build needed ---
go run ./cmd/server                       # serves a "frontend not built" page

# --- build ---
node scripts/build.mjs                    # frontend + embed + host binary → bin/
node scripts/build.mjs web                # frontend + embed only
node scripts/build.mjs binary             # binary only (assumes web is embedded)
node scripts/build.mjs release            # 6 platforms + archives + SHA256SUMS → dist/
node scripts/build.mjs status             # what would be built, and what is embedded
node scripts/build.mjs clean
```

Publishing is push-driven: every push to master runs `.github/workflows/release.yml`
(verify gate → semver bump → ONE immutable GitHub Release `vX.Y.Z` → multi-arch
images to GHCR and optionally Docker Hub, the image keeping a moving `latest`
tag while Releases do not → optional SSH deploy to a VPS running
`bash /ga/script/deploy-splash.sh` when `VPS_SSH_*` secrets are set).
`node scripts/build.mjs release` locally reproduces exactly the artifacts that
workflow uploads.

```bash
# --- test ---
go test ./...                             # Go, all packages
go test -race ./...                       # needs cgo: Linux/macOS, or Windows with gcc
go test -cover ./...
pnpm -C web test                          # frontend, single run
pnpm -C web run test:coverage

# --- lint / format ---
gofmt -l .                                # must print nothing
go vet ./...
pnpm -C web run typecheck                 # tsc --noEmit (TypeScript 7, native)
pnpm -C web run lint                      # oxlint
gofmt -w . && pnpm -C web run lint:fix    # fix both

# --- everything CI does ---
make ci                                   # or: the four commands above, in order
```

**Always run `gofmt -l .`, `go vet ./...`, `go test ./...`,
`pnpm -C web run typecheck`, `pnpm -C web run lint` and `pnpm -C web test`
before declaring a change finished.** CI runs all six and fails on any of them.

## Architecture

```
cmd/server/main.go   flags → config → logger → store.Open → server.Run(ctx)
                     Signals: SIGINT/SIGTERM cancel the context; Run drains.
internal/config      SPLASH_* parsing + validation. Load() reports every bad
                     variable at once (errors.Join) rather than one per restart.
internal/logger      Builds *slog.Logger. A configured LogFile adds a JSON sink
                     via slog.NewMultiHandler (Go 1.26).
internal/version     Stamped by -ldflags; falls back to debug.ReadBuildInfo().
internal/httpx       Transport layer: JSON (encoding/json/v2), the Error type and
                     envelope, middleware (RequestID, Logging, Recovery,
                     SecurityHeaders, CORS, NoCache). Knows nothing about the domain.
internal/auth        Credentials check (constant-time), HMAC-SHA256 session
                     tokens (JWT compact form), cookie/bearer middleware,
                     per-IP login rate limiter. Imports nothing domain-shaped.
internal/store       Domain + SQLite: Category/Project/Checkin/Skip types and
                     validation (domain.go), the schedule engine (schedule.go,
                     pure BuildStats), schema.sql + DB (db.go), one file per
                     aggregate, stats.go (ForProject/Overview/Dump), seed.go.
                     The API depends on the narrow interfaces (CategoryStore,
                     ProjectStore, CheckinStore, SkipStore, StatsStore, Exporter).
internal/api         Route table and handlers. Thin: parse → call store → encode.
                     api.fail() is the only error exit; visibility rules live in
                     visibleProject/requireAdmin.
internal/web         go:embed of the built frontend, SPA fallback, placeholder
                     page for when it has not been built.
internal/server      Assembly and lifecycle. Owns the middleware order.
web/                 React + TypeScript + Vite. src/api/client.ts is the only
                     module that calls fetch; src/lib/async.ts caches the
                     use()-based loads.
scripts/build.mjs    The single implementation of every build step.
```

Dependency direction is one-way: `cmd → server → {api, web} → {store, auth,
httpx} → config/version`. `httpx` never imports `store`; error mapping from
domain sentinels to HTTP status lives in `internal/api/errors.go`, which is the
only place that knows both.

### Data flow for a write

```
POST /api/v1/projects/{id}/checkins
  → middleware (RequestID → Logging → Recovery → SecurityHeaders → CORS → Auth)
  → api.handleCreateCheckin
      requireAdmin → visibleProject            (401 / 404 before any work)
      httpx.DecodeJSON[checkinRequest]         (body capped by MaxBytesReader)
      store.Checkins.Create → CheckinInput.Validate → INSERT
  → api.write → httpx.WriteJSON (marshals into a buffer first)
on failure → api.fail → api.mapError → httpx.WriteError
```

## Conventions

### Go

- **Errors are values, sentinels are wrapped.** `fmt.Errorf("get project %s: %w", id, err)`.
  Domain code returns `store.ErrNotFound` / `ErrConflict` / `ErrVersionConflict` /
  `ValidationErrors`; only `internal/api` knows what status those map to.
- **Validation reports everything at once.** `store.ValidationErrors` is a slice
  with an `Is` method matching `ErrValidation`, so callers write one
  `errors.Is` and clients get every field problem in a single response.
- **Optimistic concurrency lives in the SQL.** `Projects.Update` folds the
  version comparison into `UPDATE ... WHERE id = ? AND version = ?`; a handler
  must never check-then-write. `If-Match` parses to an expected version and is
  enforced the same way (412 via `ErrVersionConflict`).
- **Visibility is enforced at the API edge, once.** `requireAdmin` gates every
  write; `visibleProject` turns a private project into `ErrNotFound` for
  anonymous reads. Listings push the same predicate into SQL
  (`ProjectQuery.VisibleOnly`). No handler may skip either.
- **The schedule engine is pure.** `BuildStats` takes aggregates + skips +
  check-ins and returns verdicts; it reads no clock and runs no queries. New
  schedule rules go there with table-driven tests, not into handlers.
- **Table-driven tests**, `t.Helper()` in helpers, `t.Setenv` for environment,
  injected clocks instead of sleeping. Prefer `httptest.NewTestServer(t, h)`
  (Go 1.27) — it uses an in-memory network and cleans itself up. API tests run
  against a real `store.Open(":memory:", clock)`, not a fake.
- **Benchmarks use `for b.Loop()`** (Go 1.24+), not `for i := 0; i < b.N; i++`.
- Comments explain *why*, and every exported identifier has a doc comment.
  Match the density of the surrounding code; this repo is heavily commented on
  purpose because it is a template people read.

### TypeScript / React

- `strict`, `noUncheckedIndexedAccess`, `verbatimModuleSyntax` and
  **`erasableSyntaxOnly`** are all on. The last one forbids enums, namespaces and
  constructor parameter properties, because the Vite transform strips types
  without reading them. Write `readonly x: T` fields and assign them explicitly.
- **React 19 features are used deliberately**: `use()` with Suspense for the
  bootstrap load and for detail/day data (via `useLoaded` + the `lib/async.ts`
  cache), `useOptimistic` for quick check-ins, `useDeferredValue` for search,
  `useTransition` for post-mutation reloads (no skeleton flash). Prefer these
  over `useEffect`-and-state equivalents; oxlint's `set-state-in-effect` rule
  enforces it.
- Promises passed to `use()` are created in a `useState` initializer or a
  key-only `useMemo` and **must stay stable across renders**, or `use()`
  suspends forever. They never reject: failures are caught into discriminated
  results (`BootstrapResult`, `Loaded<T>`) so the UI can explain them without
  an error boundary.
- In tests, a component that suspends on first paint must render inside an
  awaited `act(async () => ...)` scope, or the retry never flushes (see
  `App.test.tsx`).
- `src/api/types.ts` mirrors the Go response structs by hand. **Change both
  ends together**; the comment on each says so.
- The ink palette is closed and validated (`INK_COLORS`, dataviz checks in both
  themes). Colors follow the entity and always ship with an icon or name;
  heatmap statuses use shapes (dashed/×) as well as color. Text wears ink
  tokens, never series color. The brand mark is the ink blot with splatter
  (`SealMark`, favicon.svg) — deliberately not a circle/rounded-square check.
- **One DOM, two shells.** Below 1024px the app is the phone layout (bottom
  nav, bottom sheets); at 1024px+ a sidebar rail, multi-column canvases and
  centered dialogs take over, purely via media queries. Both navigations exist
  in the DOM, so tests must use `getAllByText` for nav labels. Detail/stats
  column wrappers use `display: contents` + `order` on mobile to keep the
  phone sequence intact — don't reorder that JSX.
- Linting is **oxlint** (`.oxlintrc.json`), not ESLint.

## Gotchas

- **`internal/web/dist/.gitkeep` must stay committed.** `//go:embed all:dist`
  fails at build time if the directory is empty, so without that file a fresh
  clone does not compile. `scripts/build.mjs` preserves it when it syncs the
  build output; keep it that way if you touch the script.
- **`web/dist` and `internal/web/dist` are build output, never committed**
  (except the `.gitkeep`). Both are in `.gitignore`.
- **`.dockerignore` excludes `internal/web/dist/*`.** The Dockerfile's
  `COPY --from=web` *merges* into that directory rather than replacing it, so a
  stale host-side build would leak old hashed bundles into the image.
- **`go test -race` needs cgo.** It fails on Windows without a C toolchain
  (`-race requires cgo`); CI runs it on Linux. Don't "fix" this by dropping
  `-race` from CI.
- **Do not add `typescript-eslint`.** Its peer range is `typescript <6.1` and
  this project is on TypeScript 7 (the native compiler, which exposes no JS API
  for it to use). oxlint plus `tsc --noEmit` covers the same ground.
- **The `/api/` catch-all route matters.** Without it, an unknown API path falls
  through to the SPA handler mounted at `/` and answers `200` with `index.html`.
  `api.handleUnmatched` also produces correct `405` + `Allow` by matching the
  request path against the recorded route patterns.
- **IDs are UUIDv7 on purpose** — time-ordered, so insertion order and
  creation order agree and listings can break timestamp ties deterministically.
- **One SQLite connection means a tx owns the whole pool.** Never call a
  pool-backed store method inside an open `BeginTx` — it waits forever for the
  connection the tx is holding. `SeedDemo` creates categories/projects through
  the pool first, then opens the tx for the history bulk insert. Keep that
  ordering if you touch it.
- **`local_date` is the client's day, `checked_at` is the instant.** Periods,
  streaks and heatmaps use `local_date` (the user's timezone); only the
  interval-hours countdown and history ordering use `checked_at`. A backfill
  sets `local_date` to the requested day and `checked_at` to noon UTC of it.
  Don't "fix" one to match the other.
- **Statuses are derived, never stored.** There is no missed/skipped/streak/
  violation column. Everything comes from `BuildStats` / `PrereqSatisfied` /
  `enrichCheckins` on read, so a backfill or an undo repairs history
  retroactively (including clearing a prerequisite violation) and no migration
  is needed for schedule rules. The only stored provenance flag is
  `checkins.backfilled` (was this recorded after its credited day).
- **Prerequisite graph lives in `prerequisites(project_id, requires_id)`.**
  Cycles and self/dangling edges are rejected in `Projects.checkPrereqGraph`
  (full load + DFS, the graph is tiny). Anonymous DTOs strip private
  prerequisite ids (`hidePrivatePrereqs`) so a public gate never leaks a
  private project's existence.
- **Default credentials warn at startup.** `config.UsingDefaultCredentials()`
  drives the warning; keep it wired if you touch main. The login limiter is
  in-memory per IP — a restart resets it, which is acceptable for one admin
  account and documented as such.
- **Session cookie is browser-scoped; the token expiry is an idle timeout.**
  `SetCookie` sets no MaxAge on purpose (closing the browser forces login).
  `auth.Middleware` slides the expiry by reissuing when less than half the
  window remains; `useSessionHeartbeat` keeps open editors alive. Don't add
  MaxAge back or make the expiry absolute — both are product requirements.
- **Chrome wears celadon, data wears the inks.** `--brand` is the calm accent
  for navigation/buttons/rings/cross-project charts (`data-ink="brand"`);
  project/category colors come only from the validated `INK_COLORS`. Page
  surfaces carry `--grain` (rice-paper noise). Keep red for errors only.
- **The tab icon lives at `/icon.svg`, not `/favicon.svg`.** Browsers cache
  favicon URLs aggressively; the rename was the cache bust. PNG companions
  (`icon-192/512.png`, `apple-touch-icon.png`) are rasterized from the SVG —
  regenerate them if the mark changes, don't hand-edit.
- **HSTS is production-only.** `httpx.StrictTransportSecurity` is mounted by
  `server.New` only when `cfg.IsProduction()`; don't move it into the generic
  SecurityHeaders middleware.
- **`api.fail` is the only error exit for handlers.** Adding a direct
  `httpx.WriteError` call in a handler loses the logging and the 5xx message
  redaction.
- **`httpx.WriteJSON` marshals into a buffer before writing anything.** Do not
  "optimise" it into a streaming encoder: once the status line is on the wire an
  encoding failure leaves the client with a truncated 200.
- **CORS with `*` ignores `AllowCredentials`.** That is deliberate — "any origin
  may act as this user" is a configuration nobody meant to write.
- **Node and Go name platforms differently.** `scripts/build.mjs` maps
  `win32 → windows` and `x64 → amd64`; keep those maps if you add a target.

## Extending the domain

1. `internal/store/domain.go` — the aggregate, its input/patch types and
   validation. Keep the `ValidationErrors` shape so `api.mapError` and the
   frontend's `fieldErrorsFrom` keep working.
2. A new `internal/store/<aggregate>.go` — a narrow interface plus the
   SQLite-backed implementation; expose it from `DB` and add it to `api.Deps`.
   Schema changes go in `schema.sql` with a `user_version` bump and a migration
   step in `db.migrate`.
3. `internal/api/` — routes in `Register`, request structs, DTO converters.
   Gate writes with `requireAdmin` and reads with `visibleProject` (or the
   equivalent visibility filter) — never skip them.
4. `web/src/api/types.ts` + `web/src/api/client.ts` — the other end of the
   contract, changed in the same commit.
5. Schedule rules live in `internal/store/schedule.go` (`BuildStats`), pure and
   table-tested; the frontend only reads the verdicts (`lib/schedule.ts`).
6. Tests: `internal/store/*_test.go`, `internal/api/api_test.go`,
   `web/src/**/*.test.ts(x)`. The API tests run against a real in-memory
   SQLite database with a fixed clock; keep them that way.

## Working agreements

- **Do not commit or push unless explicitly asked.** Leave changes in the working
  tree and describe them.
- Never commit build output (`bin/`, `dist/`, `web/dist/`,
  `internal/web/dist/*`), coverage files, `.env`, the SQLite database
  (`data/`), or anything that looks like a credential.
- `pnpm-lock.yaml` changes belong in the same commit as `web/package.json`.
  CI installs with `--frozen-lockfile` and fails if they disagree.
- The Go module carries exactly one dependency (`modernc.org/sqlite`). If
  another is genuinely needed, say why in the PR description.
- When you change an API response shape, update `web/src/api/types.ts` in the
  same change and mention it in the description.
- Comments and docs in this repo explain reasoning, not mechanics. Follow that
  style; a comment that restates the code is noise.
- Run the six verification commands above before reporting work as done, and
  report actual output — including failures — rather than assuming a pass.
