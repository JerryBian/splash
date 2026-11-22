# splash

A self-hosted, mobile-first check-in habit tracker. One executable contains the
web UI and the SQLite engine, so there is no runtime, package manager or
database server to install. The UI is Chinese; the project, API and logs are
English.

[![Release](https://github.com/JerryBian/splash/actions/workflows/release.yml/badge.svg)](https://github.com/JerryBian/splash/actions/workflows/release.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Check-ins are stamps: tap once and an ink seal presses onto the card. Projects
can be scheduled daily, on chosen weekdays, weekly, monthly, every N days or
every N hours; goals can be one-tick checks or accumulated values with a unit.

## Features

- **Schedules that fit real habits** — daily, weekdays, weekly, monthly,
  interval days, interval hours; check-off or measured goals (8 cups, 30
  minutes, 5 km).
- **Backfill and skip** — fill in a forgotten day (marked 补), excuse a day
  without breaking the streak, or undo a record.
- **Prerequisites** — require other projects first (same day, chainable,
  cycle-free); checking early is marked as a violation and clears itself when
  the prerequisite is backfilled.
- **Streaks and statistics** — current/best streak, completion and on-time
  rates, a 26-week calendar, trends, activity heatmap, category distribution
  and a streak leaderboard.
- **Public and private** — visitors see public projects; one administrator
  records check-ins and manages everything. Private projects are invisible,
  not merely read-only.
- **A UI that fits the screen** — phone layout with bottom navigation and
  sheets below 1024px, desktop rail and multi-column dashboards above it;
  light, dark and system themes; reduced-motion support.
- **Plenty of personality** — 16 inks and 144 icons in a compact grouped
  picker, with recent icons remembered on the device.

## Install

### Download a release

Download the archive for your OS and architecture from the
[Releases page](https://github.com/JerryBian/splash/releases), unpack it, and
run the single binary:

| OS      | Architectures                                      |
| ------- | -------------------------------------------------- |
| Linux   | amd64, arm64, 386, armv7, ppc64le, s390x           |
| macOS   | amd64 (Intel), arm64 (Apple silicon)               |
| Windows | amd64, arm64                                       |

```bash
./splash -seed          # Linux / macOS; -seed loads demo data into an empty DB
.\splash.exe -seed      # Windows
```

Open <http://localhost:8080>. Omit `-seed` to start with an empty database.

**Default login: `admin` / `splash123`.** Change it with
`SPLASH_ADMIN_PASSWORD` and `SPLASH_AUTH_SECRET` before exposing the service.

Data lives at `SPLASH_DB_PATH` (default `data/splash.db`, relative to the working
directory). Replace the binary to upgrade; the database file is compatible
across releases and is never touched by an upgrade.

### Docker

**Linux** images are published to GHCR and, when configured, Docker Hub:

```bash
docker run -d --name splash \
  -p 8080:8080 \
  -v splash-data:/data \
  -e SPLASH_ADMIN_PASSWORD='a-real-password' \
  -e SPLASH_AUTH_SECRET="$(openssl rand -hex 32)" \
  ghcr.io/jerrybian/splash:latest
```

Open <http://localhost:8080>. The database lives on the `/data` volume; keep it
when replacing the container, and back it up like any other database.

- Tags: **`latest`** and the exact immutable version, e.g. **`1.2.3`**. Pin a
  version in production.
- Platforms: `linux/amd64`, `linux/arm64`, `linux/arm/v7`, `linux/ppc64le`,
  `linux/s390x`.
- Runs as uid 65532, with no shell and no package manager. Set
  `SPLASH_COOKIE_SECURE=true` when serving over HTTPS.

**Windows** containers are a separate image (a Windows container needs a
Windows base image and a Windows host):

```powershell
docker run -d --name splash `
  -p 8080:8080 `
  -v splash-data:C:\data `
  -e SPLASH_ADMIN_PASSWORD='a-real-password' `
  -e SPLASH_AUTH_SECRET='a-long-random-secret' `
  ghcr.io/jerrybian/splash-windows:latest
```

- Image: `ghcr.io/jerrybian/splash-windows`; same two tags: `latest` and
  the exact version.
- Platform: `windows/amd64`, based on Windows Server 2022 Nano Server. It runs
  on a matching Windows container host (Windows Server 2022, or a client with
  a compatible container runtime), not on Linux or macOS Docker.
- It runs as the default Windows container account (ContainerAdministrator),
  which can write the data volume. Keep it on a trusted host.
- Windows and macOS native binaries remain the simplest option for desktop
  use.

### Build from source

Prerequisites: Go 1.27+, Node 20.19+/22.12+ and pnpm 12.

```bash
git clone https://github.com/JerryBian/splash.git
cd splash
node scripts/build.mjs && ./bin/splash -seed   # Windows: .\bin\splash.exe -seed
```

The build embeds the frontend into the binary. For development:

```bash
node scripts/dev.mjs        # Go API :8080 + Vite :5173 with hot reload
pnpm -C web test            # frontend tests
go test ./...               # Go tests
```

On Windows, `pwsh -File scripts/run.ps1` builds, starts and opens the app with
fresh demo data. Contributors should read [AGENTS.md](AGENTS.md) for the
architecture, invariants and every verification command.

## Configuration

Everything has a working default. The most useful variables:

| Variable                 | Default          | Purpose                                             |
| ------------------------ | ---------------- | --------------------------------------------------- |
| `SPLASH_ADMIN_USERNAME`  | `admin`          | The single administrator account                    |
| `SPLASH_ADMIN_PASSWORD`  | `splash123`      | Password; **change it** before exposing the service |
| `SPLASH_AUTH_SECRET`     | development key  | Session signing secret; rotating it logs everyone out |
| `SPLASH_ENV`             | `development`    | `development`, `test` or `production`               |
| `SPLASH_HTTP_ADDR`       | `:8080`          | `host:port`                                          |
| `SPLASH_DB_PATH`         | `data/splash.db` | SQLite file; the parent directory is created        |
| `SPLASH_COOKIE_SECURE`   | `false`          | Set `true` when serving over HTTPS                  |
| `SPLASH_SESSION_TTL`     | `24h`            | Idle login timeout                                  |
| `SPLASH_SEED_DEMO`       | `false`          | Load demo data into an empty database at startup    |

Useful flags: `-version`, `-seed`, `-http-addr`, `-db-path`, `-check host:port`
(readiness probe), `-log-level` and `-pprof`.

The complete list, timeouts, CORS and log options are in
[docs/CONFIGURATION.md](docs/CONFIGURATION.md) and [.env.example](.env.example).

### Backups and data

- SQLite database: `data/splash.db` (or `SPLASH_DB_PATH`); in Docker it is
  `/data/splash.db` on the `/data` volume.
- The **Me → Data → Export** button downloads a complete JSON backup.
- Copying the SQLite file while the service is stopped is a full backup.

## API

The JSON API is versioned under `/api/v1`; see [docs/API.md](docs/API.md) for
the endpoint list, visibility rules and error envelope.

## Releasing

Every push to `master` publishes one immutable `vX.Y.Z` GitHub Release with
single-binary archives and checksums, plus Linux and Windows container images
tagged `latest` and `X.Y.Z`. The full release process, secrets, supported
platforms and GHCR
troubleshooting are in [docs/RELEASING.md](docs/RELEASING.md).

## Security and privacy

No telemetry and no third-party runtime requests. Anonymous access is
read-only and visibility-filtered; all writes require the administrator
session. Login failures are rate-limited and uniform. See
[SECURITY.md](SECURITY.md) for the full model and how to report a
vulnerability.

Design notes and internal invariants live in [docs/DESIGN.md](docs/DESIGN.md)
and [AGENTS.md](AGENTS.md).

## License

MIT — see [LICENSE](LICENSE).
