# splash · 水花打卡

A self-hosted habit tracker that feels like stamping ink on paper. Tap a habit
and a seal presses onto the card; build good habits, break bad ones, and watch
your streaks grow.

One executable holds the whole app — the web UI and the database are baked in,
so there is nothing to install beside the file itself. The interface is
Chinese; it runs comfortably on a phone, a tablet or a desktop.

[![Release](https://github.com/JerryBian/splash/actions/workflows/release.yml/badge.svg)](https://github.com/JerryBian/splash/actions/workflows/release.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

## Highlights

- **Good habits and bad habits.** Mark a project as something to build (好习惯)
  or to break (坏习惯, like 熬夜). For a bad habit a tap records a lapse, your
  streak counts the days you stayed clean, and lapses are tallied apart from
  achievements so the two never blur together.
- **Schedules that fit real life.** Daily, chosen weekdays, weekly, monthly,
  every N days or every N hours — as a simple tick or a measured goal (8 cups,
  30 minutes, 5 km).
- **Forgiving history.** Backfill a forgotten day, excuse a day without breaking
  a streak, undo a record. Browse years of history in the calendar, not just the
  last few weeks.
- **Prerequisites.** Require one habit before another (same day, chainable);
  jumping ahead is flagged and clears itself when you catch up.
- **Statistics that mean something.** Streaks, completion and clean rates, a
  stamp calendar, trends, activity heatmap, category breakdown and a leaderboard.
- **Yours alone.** The whole app sits behind one admin login. No anonymous
  access, no telemetry, no third-party requests — your data stays in a local
  SQLite file.
- **A face with personality.** Sixteen inks, a grouped icon picker, light/dark/
  system themes, and a layout that adapts from phone to widescreen.

## Quick start

Download the archive for your system from the
[Releases page](https://github.com/JerryBian/splash/releases), unpack it, and run
the single binary:

```bash
./splash -seed          # Linux / macOS  (Windows: .\splash.exe -seed)
```

Open <http://localhost:8080> and sign in. `-seed` loads a little demo data so the
first run isn't empty; omit it to start fresh.

> **Default login is `admin` / `splash123`.** Set `SPLASH_ADMIN_PASSWORD` and
> `SPLASH_AUTH_SECRET` before exposing the service to anyone else.

Linux/macOS/Windows binaries are published for the common architectures; see the
Releases page for the full list.

### Docker

```bash
docker run -d --name splash \
  -p 8080:8080 \
  -v splash-data:/data \
  -e SPLASH_ADMIN_PASSWORD='a-real-password' \
  -e SPLASH_AUTH_SECRET="$(openssl rand -hex 32)" \
  ghcr.io/jerrybian/splash:latest
```

The database lives on the `/data` volume — keep it when replacing the container.
Images carry `latest` and an exact version tag (pin a version in production). A
separate `splash-windows` image is available for Windows container hosts.

## Keeping your data

- The database is a single SQLite file (`data/splash.db` by default, `/data` in
  Docker). Copying it while the service is stopped is a complete backup.
- **Me → Data → Export** downloads a full JSON backup; **Import** restores it.
- Upgrading is just replacing the binary — the database is migrated in place and
  never discarded.

## Configuration

Everything has a working default. The settings most people touch:

| Variable                | Default          | Purpose                                  |
| ----------------------- | ---------------- | ---------------------------------------- |
| `SPLASH_ADMIN_PASSWORD` | `splash123`      | Admin password — **change it**           |
| `SPLASH_AUTH_SECRET`    | development key  | Session signing secret — **change it**   |
| `SPLASH_HTTP_ADDR`      | `:8080`          | Listen address                           |
| `SPLASH_DB_PATH`        | `data/splash.db` | Where the database lives                 |
| `SPLASH_ENV`            | `development`    | Set `production` behind HTTPS            |

The full list (timeouts, CORS, logging, sessions) is in
[docs/CONFIGURATION.md](docs/CONFIGURATION.md) and [.env.example](.env.example).

## Learn more

- [docs/API.md](docs/API.md) — the JSON API under `/api/v1`.
- [docs/DESIGN.md](docs/DESIGN.md) — how the schedule engine and UI fit together.
- [SECURITY.md](SECURITY.md) — the security model and how to report a vulnerability.
- [AGENTS.md](AGENTS.md) — architecture and invariants for contributors (and AI agents).

## License

MIT — see [LICENSE](LICENSE).
