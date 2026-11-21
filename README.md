# splash

A mobile-first check-in habit tracker: a Go 1.27 backend and a React 19 +
TypeScript 7 frontend, shipped as **one static binary** with SQLite on disk.
The compiled frontend is embedded with `go:embed`, so there is nothing to serve
from disk and nothing to keep in sync at runtime — the release file is the
whole product, no runtime, no package manager, no database server to install.

Code, comments, docs, workflows and logs are English; the user-facing UI is
Chinese (its in-app brand name translates to "water-splash check-in").

[![CI](https://github.com/JerryBian/splash/actions/workflows/ci.yml/badge.svg)](https://github.com/JerryBian/splash/actions/workflows/ci.yml)
[![Release](https://github.com/JerryBian/splash/actions/workflows/release.yml/badge.svg)](https://github.com/JerryBian/splash/actions/workflows/release.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/JerryBian/splash.svg)](https://pkg.go.dev/github.com/JerryBian/splash)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

The check-in is a stamp: tap once and an ink seal presses onto the card with a
splatter in the project's color. Eight validated inks, light and dark themes,
and a calendar heatmap that is an input, not just a picture. The brand mark is
an ink blot with flying splatter — the same shape the check-in animation makes.

**Responsive by design, not by shrink.** Below 1024px it is a phone app:
bottom navigation, bottom sheets, one column. At 1024px+ it becomes a desktop
tool: a navigation rail, project cards in two/three columns, the detail page
as calendar-plus-rail, stats as paired panels, and centered dialogs instead of
sheets. One DOM, the media query picks the shell.

## Features

- **Six schedule families** — every day, chosen weekdays, N times per week,
  N times per month, every N days, every N hours (rolling countdown). Goals
  can be counts or measured quantities with a unit (8 cups, 30 minutes, 5 km).
- **Streaks & statistics** — current/best streak per project, completion rate,
  on-time rate for interval goals, a 26-week stamp calendar, period trend bars,
  and a cross-project dashboard (activity heatmap, weekly trend, category
  distribution, streak leaderboard).
- **Skip & backfill** — excuse a day without breaking the streak; record
  a past day (backfill); undo any single check-in. Missed periods are derived from
  the schedule on read, so there is no cron job to drift.
- **Categories & CRUD** — group projects, search, archive instead of delete,
  edit with optimistic-concurrency protection (`If-Match` → `412`).
- **Two roles** — anonymous visitors browse public projects; the administrator
  (one account, credentials from environment variables) checks in and manages
  everything. Private projects are invisible to visitors: `404`, not `403`,
  and carry an explicit private/public badge everywhere an admin sees them.
- **Session model** — the cookie is browser-session scoped (closing the
  browser forces a fresh login); the token's expiry is an idle timeout
  (`SPLASH_SESSION_TTL`, default 24h) that any authenticated request slides
  forward, and open editors/check-in sheets heartbeat every few minutes so
  editing never times out mid-stroke.
- **Notes & moods** on every check-in, JSON backup export, PWA manifest,
  reduced-motion support, and a diagnostics panel showing the real last round
  trip (method, status, latency, request id).

## What is in the box

| Layer        | Choice                                          | Why                                                                              |
| ------------ | ----------------------------------------------- | -------------------------------------------------------------------------------- |
| Language     | Go 1.27                                          | `encoding/json/v2`, stdlib `uuid`, generic methods, `new(expr)`                   |
| HTTP         | `net/http` + `ServeMux` method patterns          | No router dependency; 405 handling and `{id}` wildcards are built in              |
| Storage      | SQLite via `modernc.org/sqlite`                  | Pure Go (no cgo), so the six-platform `CGO_ENABLED=0` release keeps working; WAL  |
| Auth         | Stdlib HMAC-SHA256 session tokens                | JWT-shaped, HttpOnly + SameSite=Lax cookie, per-IP login rate limit               |
| Frontend     | React 19.3, TypeScript 7, Vite 8                 | `use()` + Suspense, `useOptimistic`, `useDeferredValue`, zero runtime deps beyond React |
| Tests        | `go test` + Vitest 5 + Testing Library           | Go unit tests across 10 packages + a Vitest suite, both gated in CI                |
| Lint         | `gofmt`, `go vet`, oxlint                         | oxlint rather than ESLint: `typescript-eslint` does not support TypeScript 7 yet  |
| CI           | GitHub Actions                                   | Lint + test + type-check + a real smoke test of the embedded binary + govulncheck  |
| Release      | Push-driven semver                               | Every push: vX.Y.Z release + moving `latest` release, 6 single-binary archives, checksums, multi-arch images to GHCR and Docker Hub |
| Container    | Multi-stage → `distroless/static`                | No shell, no libc, runs as uid 65532, SQLite on a `/data` volume                   |

## Quick start

Prerequisites: **Go 1.27+** and **Node 20.19+/22.12+**, plus **pnpm** at the
version pinned by `web/package.json`'s `packageManager` field
(`npm install -g pnpm@12.4.1`). Node 25+ no longer bundles corepack, so the
Docker build and CI install pnpm explicitly at that pinned version; npm alone
works for the Go-only path but the frontend lockfile is pnpm's.

```bash
git clone https://github.com/JerryBian/splash.git
cd splash

# One command: builds the frontend, embeds it, compiles the binary, runs it.
node scripts/build.mjs && ./bin/splash -seed   # Windows: .\bin\splash.exe -seed
```

Open <http://localhost:8080>. `-seed` fills an empty database with sample
projects and ~120 days of history so the first run has something to look at;
omit it to start clean. `make build run` does the same thing if you have
`make`; on Windows the Node script is the primary interface and the Makefile is
a convenience wrapper over it.

**Sign in as `admin` / `splash123`** to check in and manage projects. Anonymous
visitors see public projects only. Both the credentials and the token signing
secret come from environment variables (`SPLASH_ADMIN_USERNAME`,
`SPLASH_ADMIN_PASSWORD`, `SPLASH_AUTH_SECRET`) — change them before exposing
the service; startup warns while the shipped defaults are in use.

A fresh clone also compiles **without** building the frontend, because
`internal/web/dist/.gitkeep` gives `go:embed` something to embed. `go run
./cmd/server` works immediately and serves a page that says how to build the
real thing:

```bash
go run ./cmd/server    # API live on :8080, placeholder page in the browser
```

## Local development

Two modes, for two different kinds of work.

### One click on Windows — build, run, demo data

```powershell
pwsh -File scripts/run.ps1                 # build + start + fresh demo data + open browser
pwsh -File scripts/run.ps1 -SkipBuild      # quick restart, skip the build
pwsh -File scripts/run.ps1 -KeepData       # keep the demo data from last run
pwsh -File scripts/run.ps1 -Port 9000 -Foreground   # run in this console, Ctrl+C stops
```

The script locates Go even when it is not on PATH, builds through
`scripts/build.mjs`, and starts the binary with `-seed`. It force-starts:
existing splash instances and whatever process holds the port are stopped
first (system-critical processes are refused with a hint to change `-Port`).
Demo data lives in a separate `data/demo.db` that is recreated on each run
(unless `-KeepData`), so your real database (`data/splash.db`) is never
touched. Pass `-DbPath` to use your own file; it is never deleted, and `-seed`
only fills it when empty.

### Frontend work — hot reload

```bash
node scripts/dev.mjs      # or: make dev
```

This starts both processes with prefixed output and stops both on `Ctrl+C`:

- the Go server on `127.0.0.1:8080`, logs as readable text at debug level
- Vite on <http://localhost:5173>, proxying `/api` to the Go server

**Work in the Vite tab.** The browser talks to one origin, so no CORS
configuration is involved. The proxy target is `vite.config.ts`; change it there
if you move `SPLASH_HTTP_ADDR`.

### Backend work — the real artifact

```bash
node scripts/build.mjs web && go run ./cmd/server
```

This is what production runs: one binary, frontend included. Use it when the
behaviour you are checking involves embedding, caching headers, CSP or the SPA
fallback — none of which the Vite dev server exercises.

## Project layout

```
cmd/server/            main: flags, signals, exit codes, self health probe
internal/
  config/              SPLASH_* parsing, validation, flag overrides
  logger/              slog construction, including the file sink (MultiHandler)
  version/             build metadata stamped by -ldflags
  httpx/               JSON (encoding/json/v2), error envelope, middleware
  auth/                credentials check, HMAC session tokens, login rate limit
  store/               domain: categories, projects, check-ins, skips, the
                       schedule engine (streaks/heatmap/trend) and SQLite
  api/                 route table, handlers, visibility rules, error mapping
  web/                 go:embed of the built frontend + SPA fallback handler
  server/              assembly: mux + middleware + listen + graceful shutdown
scripts/build.mjs      the build orchestrator every entry point funnels through
scripts/dev.mjs        both dev servers in one terminal
web/                   React + TypeScript + Vite source
.github/workflows/     ci.yml (every push/PR), release.yml (every tag)
```

Nothing outside `internal/web` knows the frontend exists, and nothing in `web/`
knows how it is served.

## Configuration

Everything has a working default. Variables are read once at startup; flags win
over variables, because an explicit argument is a more recent statement of
intent than something set at deploy time.

### Environment variables

| Variable                       | Default     | Notes                                                        |
| ------------------------------ | ----------- | ------------------------------------------------------------ |
| `SPLASH_ENV`                   | development | `development`, `test` or `production`                          |
| `SPLASH_HTTP_ADDR`             | `:8080`     | `host:port`; use `:0` for an ephemeral port in tests            |
| `SPLASH_READ_TIMEOUT`          | `15s`       | whole request, body included                                   |
| `SPLASH_READ_HEADER_TIMEOUT`   | `5s`        | the one that protects against slowloris                        |
| `SPLASH_WRITE_TIMEOUT`         | `30s`       |                                                                |
| `SPLASH_IDLE_TIMEOUT`          | `90s`       | keep-alive                                                     |
| `SPLASH_SHUTDOWN_TIMEOUT`      | `20s`       | grace period for in-flight requests during a rolling restart   |
| `SPLASH_MAX_REQUEST_BODY_BYTES`| `1048576`   | hard cap 32 MiB                                                |
| `SPLASH_LOG_FORMAT`            | `text`      | `text` or `json`; production defaults to `json`                |
| `SPLASH_LOG_LEVEL`             | `info`      | `debug`, `info`, `warn`, `error`                               |
| `SPLASH_LOG_FILE`              | *(unset)*   | extra JSON sink; both sinks are driven by one `MultiHandler`   |
| `SPLASH_LOG_SOURCE`            | `false`     | add file and line to every record                              |
| `SPLASH_CORS_ALLOWED_ORIGINS`  | *(unset)*   | comma-separated; unset means no CORS headers at all            |
| `SPLASH_CORS_MAX_AGE`          | *(unset)*   | preflight cache, e.g. `10m`; unset omits the header            |
| `SPLASH_ENABLE_PPROF`          | `false`     | mounts `/debug/pprof/`; never enable on a public interface     |
| `SPLASH_ADMIN_USERNAME`        | `admin`     | the single administrator account                               |
| `SPLASH_ADMIN_PASSWORD`        | `splash123` | 8..128 characters; **change it** before exposing the service   |
| `SPLASH_AUTH_SECRET`           | dev default | 16..256 chars signing sessions; rotating it logs everyone out  |
| `SPLASH_SESSION_TTL`           | `24h`       | login IDLE timeout, 1h..720h; activity slides it forward       |
| `SPLASH_COOKIE_SECURE`         | `false`     | set `true` when serving over HTTPS                             |
| `SPLASH_DB_PATH`               | `data/splash.db` | SQLite file; parent dir auto-created; `:memory:` for tests |
| `SPLASH_SEED_DEMO`             | `false`     | demo data into an empty database at startup (`-seed` flag too) |

`.env.example` documents the same list. The service reads the process
environment directly — there is no `.env` loader, because a container already
has one.

### Flags

```
-version              print build metadata and exit
-check <host:port>    probe /api/v1/health/ready and exit 0 or 1
-http-addr            override SPLASH_HTTP_ADDR
-log-format           text|json
-log-level            debug|info|warn|error
-log-file             also write JSON logs to this file
-db-path              override SPLASH_DB_PATH
-seed                 seed demo data into an empty database
-pprof                mount /debug/pprof/
```

`-check` exists because the runtime image is distroless: no shell and no curl,
so the container `HEALTHCHECK` is the binary probing itself.

Exit codes follow the Go toolchain's convention: `0` success, `1` runtime
failure, `2` usage or configuration error. A boot with several bad variables
reports all of them at once rather than one per restart.

## HTTP API

| Method   | Path                              | Access  | Purpose                                              |
| -------- | --------------------------------- | ------- | ---------------------------------------------------- |
| `GET`    | `/api/v1/health`                  | public  | Liveness. Process state only — never touches a dependency |
| `GET`    | `/api/v1/health/ready`            | public  | Readiness. 503 when a dependency check fails          |
| `GET`    | `/api/v1/version`                 | public  | Build metadata, environment, uptime                   |
| `POST`   | `/api/v1/auth/login`              | public  | Sign in; sets the session cookie. Rate-limited per IP |
| `POST`   | `/api/v1/auth/logout`             | public  | Clear the session                                     |
| `GET`    | `/api/v1/auth/me`                 | public  | The request's role and username                       |
| `GET`    | `/api/v1/categories`              | public† | Categories (visitors: only those holding public projects) |
| `POST`   | `/api/v1/categories`              | admin   | Create                                                |
| `PATCH`  | `/api/v1/categories/{id}`         | admin   | Replace name/icon/color                               |
| `DELETE` | `/api/v1/categories/{id}`         | admin   | Delete; its projects become uncategorized             |
| `GET`    | `/api/v1/projects`                | public† | List with current status. `date`, `archived`, `category`, `q` |
| `POST`   | `/api/v1/projects`                | admin   | Create. `201` with `Location` and `ETag`               |
| `GET`    | `/api/v1/projects/{id}`           | public† | Read. Honours `If-None-Match` → `304`                  |
| `PATCH`  | `/api/v1/projects/{id}`           | admin   | Partial update. `If-Match` enforced in SQL → `412`     |
| `DELETE` | `/api/v1/projects/{id}`           | admin   | Delete; check-ins and skips cascade                    |
| `GET`    | `/api/v1/projects/{id}/checkins`  | public† | History. `from`, `to`, `limit`                         |
| `POST`   | `/api/v1/projects/{id}/checkins`  | admin   | Check in / backfill (`date` in the past)               |
| `DELETE` | `/api/v1/checkins/{id}`           | admin   | Undo one check-in                                      |
| `GET`    | `/api/v1/projects/{id}/skips`     | public† | Excused days                                           |
| `POST`   | `/api/v1/projects/{id}/skips`     | admin   | Excuse a day; duplicate → `409`                        |
| `DELETE` | `/api/v1/projects/{id}/skips/{date}` | admin | Un-excuse a day                                       |
| `GET`    | `/api/v1/projects/{id}/stats`     | public† | Summary, totals, heatmap (`weeks`), trend, recent, skips |
| `GET`    | `/api/v1/stats/overview`          | public† | Cross-project dashboard                                |
| `GET`    | `/api/v1/export`                  | admin   | Full JSON backup, offered as a download                |
| `GET`    | `/debug/pprof/*`                  | public  | Only with `SPLASH_ENABLE_PPROF=true`                  |

† Public reads are visibility-filtered: anonymous visitors see public,
non-archived projects only, and a private project answers `404` on every read
path so its existence is not leaked. All writes require the admin session
(cookie or `Authorization: Bearer`); anonymous writes answer `401`.

Every other `/api/*` request gets a JSON `404`, and a known path with an
unregistered method gets a JSON `405` with a correct `Allow` header. Unknown
paths outside `/api/` fall through to the SPA, which is what makes client-side
routing survive a hard refresh.

### Error envelope

```json
{
  "error": {
    "code": "validation_failed",
    "message": "the request was rejected: 2 field problem(s)",
    "details": [
      { "field": "title", "message": "is required" },
      { "field": "tags[1]", "message": "must not be empty" }
    ],
    "requestId": "4b758319-db2c-492d-87f5-a3927e7f2672"
  }
}
```

`code` is the stable machine-readable contract; `message` is safe to show a
user. For 5xx responses the message is replaced by a generic one and the cause
goes to the log instead — there is no code path by which an internal error
string reaches a response body. `requestId` is echoed in the `X-Request-ID`
response header and appears on the matching log line.

Codes: `invalid_json`, `invalid_argument`, `not_found`, `conflict`,
`validation_failed`, `payload_too_large`, `method_not_allowed`,
`precondition_failed`, `timeout`, `internal_error`, `unauthorized`,
`rate_limited`.

### A tour

```bash
curl -s localhost:8080/api/v1/health

# Sign in; the session rides in an HttpOnly cookie
curl -s -c jar -X POST localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"splash123"}'

# A project: 8 cups of water a day, public
curl -s -b jar -X POST localhost:8080/api/v1/projects \
  -H 'Content-Type: application/json' \
  -d '{"name":"Water","icon":"💧","color":"blue","schedule":"daily","target":8,"unit":"cups","startDate":"2026-11-01"}'

# Check in three cups, then read the stats
curl -s -b jar -X POST localhost:8080/api/v1/projects/$ID/checkins \
  -H 'Content-Type: application/json' -d '{"value":3,"mood":"🙂"}'
curl -s -b jar "localhost:8080/api/v1/projects/$ID/stats?weeks=26"

# Excuse a day without breaking the streak
curl -s -b jar -X POST localhost:8080/api/v1/projects/$ID/skips \
  -H 'Content-Type: application/json' -d '{"date":"2026-11-20","reason":"business trip"}'

# Conditional write: send back the ETag you read
curl -s -b jar -X PATCH localhost:8080/api/v1/projects/$ID \
  -H 'Content-Type: application/json' -H 'If-Match: "v1"' \
  -d '{"target":10}'
```

Two behaviours come from `encoding/json/v2` (standard since Go 1.27) and are
worth knowing: a nil slice encodes as `[]` rather than `null`, so clients never
need a null guard; and duplicate object members are rejected rather than
silently resolved last-one-wins.

## Testing

```bash
go test ./...                  # Go
go test -race ./...            # needs cgo: Linux/macOS, or Windows with gcc
pnpm -C web test               # frontend, single run
pnpm -C web run test:watch     # frontend, watch mode
make test                      # both
make ci                        # lint + test + build, exactly as CI does

# End-to-end against a running instance: creates its own data, walks auth,
# visibility, check-in/backfill/skip/undo, If-Match, stats, export and the SPA,
# then cleans up. --rate additionally exercises the login limiter (opt-in: it
# locks the admin login for the limiter window).
node scripts/smoke.mjs http://127.0.0.1:8080
```

`make cover` writes both coverage reports and prints the Go total.

`-race` is worth the setup: `internal/store` is concurrent by design and
`internal/server` is tested for graceful drain of an in-flight request, which is
the kind of thing only the race detector reliably catches.

## Continuous integration

`.github/workflows/ci.yml` runs on every push to `main` and every pull request,
in three parallel jobs plus one build check:

- **Go** — `gofmt -l`, `go vet`, `go test -race` with coverage, an informational
  `govulncheck` pass, and a build with no frontend present (which is what a
  fresh clone does)
- **Frontend** — install with a frozen lockfile, `tsc --noEmit`, oxlint, Vitest
  with coverage, then a production build staged for embedding
- **Embedded binary** — takes the frontend artifact, builds the real binary and
  smoke-tests it: starts it, probes health and readiness, asserts the SPA is
  actually embedded, refuses an anonymous write, logs in, creates a project,
  checks in, reads the stats back, and checks that an unknown API path returns
  JSON rather than HTML
- **Container image** — builds the Dockerfile for `linux/amd64` without pushing,
  so a broken image fails on the pull request instead of on release day

Superseded runs are cancelled automatically. `dependabot.yml` keeps Go modules,
npm packages, GitHub Actions and Docker base images current.

## Releasing

**Every push to `main` ships a version.** `.github/workflows/release.yml`:

1. runs the full verify gate (gofmt, vet, `go test -race`, type-check, oxlint,
   Vitest) — a push that breaks tests publishes nothing
2. computes the next **semver** version: patch bump per push, `[minor]` or
   `[major]` in the head commit message bumps that component, first release
   ever is `v0.1.0`. Pushing a `v*` tag publishes exactly that tag instead;
   `workflow_dispatch` accepts an explicit version
3. cross-compiles **linux, darwin and windows × amd64 and arm64** with
   `CGO_ENABLED=0 -trimpath` — each archive holds ONE self-contained binary
   (embedded UI, pure-Go SQLite), plus `README.md`, `LICENSE`, `AGENTS.md`
4. verifies `SHA256SUMS` and fails if any platform is missing
5. publishes **two GitHub Releases**: the immutable `vX.Y.Z` and a moving
   `latest` (previous `latest` release/tag replaced, so the "Latest" badge
   always points at the newest build); both carry all six archives + checksums
6. builds `linux/amd64` + `linux/arm64` manifest lists and pushes them to
   **`ghcr.io/<owner>/splash`** and, when the Docker Hub secrets exist,
   **`docker.io/<user>/splash`**, tagged `X.Y.Z`, `X.Y`, `X`, `sha-<commit>`,
   `latest`

Runs queue (`cancel-in-progress: false`), so rapid pushes each get their own
version instead of cancelling one another. Build stages are pinned to
`$BUILDPLATFORM` and cross-compile, so multi-arch needs no QEMU emulation.

### Secrets

| Secret               | Required | Purpose                                             |
| -------------------- | -------- | --------------------------------------------------- |
| `GITHUB_TOKEN`       | automatic| Tag push, Release upload, GHCR push (workflow declares `contents: write` / `packages: write`) |
| `GHCR_TOKEN`         | optional | PAT with `write:packages`; overrides `GITHUB_TOKEN` for the GHCR login when the repository/package settings block it (see below) |
| `DOCKERHUB_USERNAME` | optional | Docker Hub namespace; omit to publish to GHCR only   |
| `DOCKERHUB_TOKEN`    | optional | Docker Hub access token (not the account password)   |

Nothing else to configure: no signing keys, no cloud credentials. The Docker
Hub half is skipped cleanly when those two secrets are absent — the job
publishes to GHCR and prints a notice instead of failing.

### Troubleshooting: GHCR push denied (`permission_denied: write_package`)

The workflow already requests `packages: write`, so this denial comes from
repository/package settings, not from the pipeline. Fix it once, in order:

1. **Repository workflow permissions**: Settings → Actions → General →
   *Workflow permissions* → select **Read and write permissions** → Save.
   A read-only default can cap what `GITHUB_TOKEN` is granted.
2. **Package Actions access**: if the package already exists, open
   `ghcr.io/<owner>/splash` → Package settings → *Manage Actions access* →
   make sure this repository is listed with the **Write** role (add it if
   missing). Alternatively delete the empty package and let the next run
   recreate it.
3. **Or bypass both**: create a classic PAT with `write:packages` (and
   `read:packages`) and store it as the `GHCR_TOKEN` secret; the GHCR login
   prefers it over `GITHUB_TOKEN`.

After fixing, use **Re-run failed jobs** on the failed run to publish the
images for the version that already has its GitHub Releases — no new commit
or version bump needed.

## Security & privacy

[SECURITY.md](SECURITY.md) is the full model. The short version: one admin
account from environment variables with constant-time comparison and per-IP
login rate limiting; browser-session cookies with a sliding idle timeout;
private projects answer `404` to anonymous requests on every path; CSP and
the usual hardening headers on every response, HSTS in production; SQLite
files created owner-only; no telemetry and no third-party runtime requests;
`govulncheck` + Dependabot in CI. Report vulnerabilities privately via GitHub
Security Advisories.

### Doing it by hand

The release script is the same one CI runs, so a release is reproducible
locally:

```bash
SPLASH_VERSION=v1.2.3 node scripts/build.mjs release
ls dist/            # 6 archives + SHA256SUMS
(cd dist && sha256sum --check --strict SHA256SUMS)
```

## Docker

```bash
docker build -t splash:dev .
docker run --rm -p 8080:8080 -v splash-data:/data splash:dev

# With the version stamped the way a release would stamp it:
docker build \
  --build-arg VERSION=v1.2.3 \
  --build-arg COMMIT=$(git rev-parse HEAD) \
  --build-arg DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
  -t splash:1.2.3 .

docker run --rm -p 8080:8080 -v splash-data:/data \
  -e SPLASH_ADMIN_PASSWORD='a-real-password' \
  -e SPLASH_AUTH_SECRET="$(openssl rand -hex 32)" \
  -e SPLASH_COOKIE_SECURE=true \
  splash:1.2.3
```

The image is `gcr.io/distroless/static-debian12:nonroot`: no shell, no package
manager, uid 65532. SQLite lives at `/data/splash.db` on a volume owned by that
uid; with a bind mount instead, the host directory must be writable by 65532.
For hardened hosts add `--read-only` — apart from the `/data` volume nothing
writes to disk. To poke at a running container, use the binary's own flags
from outside (`splash -check 127.0.0.1:8080`) rather than `docker exec`.

Published images are multi-arch manifest lists (`linux/amd64` + `linux/arm64`);
`docker pull` picks the right one automatically. Windows and macOS users take
the single-binary archives from the Releases page instead.

## Design decisions worth knowing

- **One Go dependency, on purpose.** The router, JSON, UUIDs, logging, auth and
  validation are standard library as of Go 1.27. The single exception is
  `modernc.org/sqlite`: SQLite was a hard requirement, and the pure-Go driver
  keeps the `CGO_ENABLED=0` six-platform release working (a cgo driver would
  break it, and need a C toolchain on every Windows dev machine).
- **SQLite with one connection.** Writes serialize in SQLite anyway; a single
  connection removes `SQLITE_BUSY` entirely while every query stays in the
  microsecond range. WAL + `synchronous=NORMAL` + covering indexes
  (`project_id, local_date` and `project_id, checked_at`) carry the stats.
  The corollary: never run a pool query inside an open transaction — the tx
  holds the only connection the pool could hand out (the demo seed creates its
  projects first, then opens the tx for the history bulk insert).
- **Statuses are derived, never stored.** Streaks, misses and heatmaps are
  computed from check-ins + skips + the schedule on every read
  (`store.BuildStats`, a pure function). Nothing to backfill, no cron to drift,
  and a backfilled check-in repairs a streak retroactively.
- **The client's day is the day.** Every period computation uses the
  `local_date` the client sends (its own timezone), not the server's clock;
  `?date=` lets the UI look at yesterday. Instants (`checked_at`) drive only
  the interval-hours countdown and history ordering.
- **Private means invisible.** Anonymous reads of a private project answer
  `404` on every path — list, detail, stats, check-ins, skips — so existence is
  not leaked. Writes answer `401`. Login failures are uniform and rate-limited
  per IP; the password is compared constant-time on SHA-256 hashes.
- **The frontend is embedded, not served.** One artifact to deploy, one version
  to reason about, and no way for the UI and API to drift apart. `web/dist` and
  `internal/web/dist` are both gitignored except for the `.gitkeep` that keeps
  `go:embed` resolvable.
- **Liveness never checks a dependency.** A liveness probe that fails when the
  database is down makes the orchestrator restart every replica during an
  outage. Dependency state is reported by `/health/ready`, which takes the
  instance out of rotation without killing it.
- **Errors leave through one function.** `api.fail` maps, logs and writes; a new
  handler cannot forget to log or leak an internal message.
- **oxlint, not ESLint.** `typescript-eslint` declares `typescript <6.1` as a
  peer, so it cannot run against TypeScript 7's native compiler. oxlint parses
  TypeScript itself, needs no type information, and runs in milliseconds.
  `tsc --noEmit` remains the type-checking step.
- **CORS is off by default.** The dev setup needs none (Vite proxies), and
  enabling it is a deliberate act with an explicit origin list. A wildcard
  combined with `AllowCredentials` is refused rather than honoured. The session
  cookie is SameSite=Lax, which together with the JSON-only API is the CSRF
  posture.
- **The palette is computed, not eyeballed.** The eight project inks pass the
  dataviz validator's lightness, chroma, CVD-separation and contrast checks in
  both themes; every colored mark also carries an icon or a name, and heatmap
  statuses use shapes (dashed = skipped, × = missed) as well as color. The UI
  chrome deliberately does NOT use them: the accent is a calm celadon
  (celadon), red survives only as the error color and one optional project ink,
  and page surfaces carry a 4%-alpha rice-paper grain so large areas stay
  easy on the eyes.
- **Sessions slide, cookies don't persist.** `auth.Middleware` reissues the
  token once less than half the idle window remains, so activity keeps a
  session alive indefinitely while silence ends it; the cookie has no MaxAge,
  so closing the browser always forces a fresh login. Editing surfaces
  (`useSessionHeartbeat`) ping every 4 minutes to hold the session open.

## Domain model

```
Category 1──* Project 1──* Checkin   (value in the project's unit, note, mood,
                    └──* Skip        local_date + checked_at instant)
```

A `Project` carries one of six schedules; `target` means units per period
(day / ISO week / calendar month / N-day window), except `interval_hours`,
which has a rolling countdown instead of periods. `store/schedule.go` turns
check-ins and skips into summaries, streaks, totals, heatmaps and trend bars;
it is pure and table-tested. Extending the domain means: add the aggregate in
`internal/store`, its handlers in `internal/api`, and the mirror types in
`web/src/api/types.ts` — the two ends of the contract change together.

[AGENTS.md](AGENTS.md) has the short version of all of this for coding agents,
including the invariants that are easy to break by accident.

## License

MIT — see [LICENSE](LICENSE).
