# Configuration

Everything has a working default. Variables are read once at startup; command
line flags win over environment variables. A container already has an
environment, so there is no `.env` loader — set variables through your process
manager, `docker run -e`, or your orchestrator.

## Environment variables

| Variable                        | Default          | Notes                                                          |
| ------------------------------- | ---------------- | -------------------------------------------------------------- |
| `SPLASH_ENV`                    | `development`    | `development`, `test` or `production`                          |
| `SPLASH_HTTP_ADDR`              | `:8080`          | `host:port`; `:0` for an ephemeral port in tests                |
| `SPLASH_READ_TIMEOUT`           | `15s`            | Whole request, body included                                   |
| `SPLASH_READ_HEADER_TIMEOUT`    | `5s`             | Header read timeout (slowloris protection)                     |
| `SPLASH_WRITE_TIMEOUT`          | `30s`            | Response write timeout                                         |
| `SPLASH_IDLE_TIMEOUT`           | `90s`            | Keep-alive idle timeout                                        |
| `SPLASH_SHUTDOWN_TIMEOUT`       | `20s`            | Grace period for in-flight requests during a restart           |
| `SPLASH_MAX_REQUEST_BODY_BYTES` | `1048576`        | Hard cap 32 MiB                                                |
| `SPLASH_LOG_FORMAT`             | `text`           | `text` or `json`; production defaults to `json`                |
| `SPLASH_LOG_LEVEL`              | `info`           | `debug`, `info`, `warn`, `error`                               |
| `SPLASH_LOG_FILE`               | *(unset)*        | Extra JSON log sink                                            |
| `SPLASH_LOG_SOURCE`             | `false`          | Add file and line to every log record                          |
| `SPLASH_CORS_ALLOWED_ORIGINS`   | *(unset)*        | Comma-separated origins; unset means no CORS headers           |
| `SPLASH_CORS_MAX_AGE`           | *(unset)*        | Preflight cache, e.g. `10m`; unset omits the header            |
| `SPLASH_ENABLE_PPROF`           | `false`          | Mounts `/debug/pprof/`; never enable on a public interface     |
| `SPLASH_ADMIN_USERNAME`         | `admin`          | The single administrator account                               |
| `SPLASH_ADMIN_PASSWORD`         | `splash123`      | 8–128 characters; change before exposing the service           |
| `SPLASH_AUTH_SECRET`            | development key  | 16–256 characters; rotating it logs everyone out               |
| `SPLASH_SESSION_TTL`            | `24h`            | Idle login timeout, 1h–720h; activity slides it forward        |
| `SPLASH_COOKIE_SECURE`          | `false`          | Set `true` when serving over HTTPS                             |
| `SPLASH_DB_PATH`                | `data/splash.db` | SQLite file; parent directory is created; `:memory:` for tests |
| `SPLASH_SEED_DEMO`              | `false`          | Demo data into an empty database at startup                    |

`.env.example` contains the same list and can be used as a template.

## Flags

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

`-check` exists because the container image is distroless: there is no shell
and no curl, so `HEALTHCHECK` is the binary probing itself.

Exit codes follow the Go toolchain's convention: `0` success, `1` runtime
failure, `2` usage or configuration error. A boot with several bad variables
reports all of them at once.

## Docker environment

```bash
docker run -d --name splash \
  -p 8080:8080 \
  -v splash-data:/data \
  -e SPLASH_ADMIN_PASSWORD='a-real-password' \
  -e SPLASH_AUTH_SECRET="$(openssl rand -hex 32)" \
  ghcr.io/jerrybian/splash:latest
```

The Linux image sets `SPLASH_HTTP_ADDR=:8080`, `SPLASH_ENV=production`,
`SPLASH_LOG_FORMAT=json` and `SPLASH_DB_PATH=/data/splash.db` by default. Mount
`/data` as a volume to keep the database across container replacement.

The Windows image (`splash-windows`) uses the same defaults with
`SPLASH_DB_PATH=C:/data/splash.db`; mount `C:\data` instead.
