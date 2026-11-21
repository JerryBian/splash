# Security & privacy

splash is a self-hosted, single-binary habit tracker. This document
is the security model in one place; the code comments carry the reasoning.

## Reporting a vulnerability

Please report privately via **GitHub Security Advisories**
("Report a vulnerability" on the repository's Security tab). Do not open a
public issue for an unfixed vulnerability. We aim to acknowledge within a
week and to ship a fix or mitigation before public disclosure.

## Threat model

One administrator account manages personal habit data; anonymous visitors
read a public subset. The service is expected to run on a trusted host or
behind a TLS-terminating reverse proxy. It is not a multi-tenant system.

## Authentication & sessions

- One admin account from `SPLASH_ADMIN_USERNAME` / `SPLASH_ADMIN_PASSWORD`.
  Passwords are compared constant-time over SHA-256 hashes; login failures
  answer uniformly (no username/password oracle) and are rate-limited per IP
  (10 attempts / 15 min, in-memory).
- Sessions are HMAC-SHA256 signed tokens (JWT compact form) in an
  `HttpOnly`, `SameSite=Lax` cookie; `SPLASH_COOKIE_SECURE=true` adds `Secure`
  for HTTPS deployments. `Authorization: Bearer` is accepted for scripts.
- The cookie is **browser-session scoped**: closing the browser forces a new
  login. The token expiry is an **idle timeout** (`SPLASH_SESSION_TTL`,
  default 24h) slid forward by activity, so editing never expires mid-stroke.
- Rotating `SPLASH_AUTH_SECRET` invalidates every issued session immediately.
- Startup warns while the shipped default password or secret is in use.
  **Change both before exposing the service.**

## Authorization & privacy of private projects

- Every write requires the admin role (`401` otherwise).
- Private projects are invisible to anonymous requests on *every* read path
  (list, detail, stats, check-ins, skips, categories roll-up): `404`, never
  `403`, so existence is not leaked.
- CSRF posture: `SameSite=Lax` cookie + JSON-only API; no state-changing GET.
- No telemetry, no analytics, no third-party requests at runtime. The display
  font is self-hosted. All data lives in the local SQLite file; `/export`
  (admin) gives a full backup, and deleting a project cascades its records.

## Transport & headers

- `Content-Security-Policy` (self-only scripts/fonts/connect, no inline
  scripts), `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`,
  `Referrer-Policy: strict-origin-when-cross-origin`, `Permissions-Policy`
  denying camera/mic/geolocation, `Cross-Origin-Opener-Policy: same-origin`.
- Production adds `Strict-Transport-Security: max-age=31536000; includeSubDomains`.
- CORS is off by default; enabling it is an explicit origin allow-list, and
  `*` never combines with credentials.
- Request bodies capped (1 MiB default, 32 MiB hard limit), header size and
  repetition capped, read/header/write/idle timeouts set, graceful drain on
  shutdown. `/debug/pprof/` is off by default and warns when enabled.

## Storage

- SQLite file and directory are created owner-only (`0700` dir, `0600` file
  and WAL sidecars, best effort). The container runs as uid 65532 on a
  distroless static base with no shell; `/data` is a volume owned by that uid.
  For hardened hosts run the container with `--read-only` plus the `/data`
  volume — nothing else writes to disk.
- WAL + `synchronous=NORMAL`: durable across process crashes; a machine-level
  power loss can lose the last transactions, which is the standard SQLite
  trade-off for a personal tracker.

## Dependencies & supply chain

- One Go dependency (`modernc.org/sqlite`, pure Go, pinned in `go.sum`);
  frontend runtime deps are React only. Lockfiles are frozen in CI.
- CI runs `govulncheck` informationally on every push; Dependabot opens weekly
  update PRs for Go modules, npm, Actions and Docker bases.
- Releases are built by GitHub Actions from the pushed commit with
  `CGO_ENABLED=0 -trimpath`, checksummed (`SHA256SUMS`), and published to
  GitHub Releases plus GHCR / Docker Hub. Binaries are not signed yet; verify
  checksums against the release page.

## Known limitations

- Single admin account by design; no per-user data isolation.
- The login rate limiter is per-process and resets on restart.
- Session tokens are stateless: logout clears the cookie but cannot revoke a
  copied token before its expiry — rotate `SPLASH_AUTH_SECRET` to revoke all.
- HTTP (no TLS) is fine on loopback; use a TLS proxy or `SPLASH_COOKIE_SECURE`
  plus HTTPS for anything network-exposed.
