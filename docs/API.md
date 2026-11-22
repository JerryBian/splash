# HTTP API

Base URL: `http://localhost:8080/api/v1`. Requests and responses are JSON.
Reads are public where marked; all writes require the administrator session
(cookie or `Authorization: Bearer`).

## Endpoints

| Method   | Path                                 | Access  | Purpose                                                    |
| -------- | ------------------------------------ | ------- | ---------------------------------------------------------- |
| `GET`    | `/health`                            | public  | Liveness. Process state only; never touches a dependency    |
| `GET`    | `/health/ready`                      | public  | Readiness. `503` when a dependency check fails              |
| `GET`    | `/version`                           | public  | Build metadata, environment, uptime                         |
| `POST`   | `/auth/login`                        | public  | Sign in; sets the session cookie. Rate-limited per IP       |
| `POST`   | `/auth/logout`                       | public  | Clear the session                                           |
| `GET`    | `/auth/me`                           | public  | The request's role and username                             |
| `GET`    | `/categories`                        | public† | Categories (visitors: only those holding public projects)   |
| `POST`   | `/categories`                        | admin   | Create                                                      |
| `PATCH`  | `/categories/{id}`                   | admin   | Replace name/icon/color                                     |
| `DELETE` | `/categories/{id}`                   | admin   | Delete; its projects become uncategorized                   |
| `GET`    | `/projects`                          | public† | List with current status. `date`, `archived`, `category`, `q` |
| `POST`   | `/projects`                          | admin   | Create. `201` with `Location` and `ETag`                    |
| `GET`    | `/projects/{id}`                     | public† | Read. Honours `If-None-Match` → `304`                       |
| `PATCH`  | `/projects/{id}`                     | admin   | Partial update. `If-Match` enforced in SQL → `412`          |
| `DELETE` | `/projects/{id}`                     | admin   | Delete; check-ins and skips cascade                         |
| `GET`    | `/projects/{id}/checkins`            | public† | History. `from`, `to`, `limit`                              |
| `POST`   | `/projects/{id}/checkins`            | admin   | Check in / backfill (`date` in the past)                    |
| `DELETE` | `/checkins/{id}`                     | admin   | Undo one check-in                                           |
| `GET`    | `/projects/{id}/skips`               | public† | Excused days                                                |
| `POST`   | `/projects/{id}/skips`               | admin   | Excuse a day; duplicate → `409`                             |
| `DELETE` | `/projects/{id}/skips/{date}`        | admin   | Un-excuse a day                                             |
| `GET`    | `/projects/{id}/stats`               | public† | Summary, totals, heatmap (`weeks`), trend, recent, skips    |
| `GET`    | `/stats/overview`                    | public† | Cross-project dashboard                                     |
| `GET`    | `/export`                            | admin   | Full JSON backup, offered as a download                     |
| `GET`    | `/debug/pprof/*`                     | public  | Only with `SPLASH_ENABLE_PPROF=true`                        |

† Public reads are visibility-filtered: anonymous visitors see public,
non-archived projects only, and a private project answers `404` on every read
path so its existence is not leaked. All writes answer `401` without the admin
session.

Every other `/api/*` request gets a JSON `404`; a known path with an
unregistered method gets a JSON `405` with a correct `Allow` header. Unknown
paths outside `/api/` fall through to the SPA, which is what makes client-side
routing survive a hard refresh.

## Error envelope

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
goes to the log instead. `requestId` is echoed in the `X-Request-ID` response
header and appears on the matching log line.

Codes: `invalid_json`, `invalid_argument`, `not_found`, `conflict`,
`validation_failed`, `payload_too_large`, `method_not_allowed`,
`precondition_failed`, `timeout`, `internal_error`, `unauthorized`,
`rate_limited`.

## Example

```bash
curl -s localhost:8080/api/v1/health

# Sign in; the session rides in an HttpOnly cookie.
curl -s -c jar -X POST localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"splash123"}'

# A project: 8 cups of water a day, public.
curl -s -b jar -X POST localhost:8080/api/v1/projects \
  -H 'Content-Type: application/json' \
  -d '{"name":"Water","icon":"💧","color":"blue","schedule":"daily","target":8,"unit":"cups","startDate":"2026-11-01"}'

# Check in three cups, then read the statistics.
curl -s -b jar -X POST localhost:8080/api/v1/projects/$ID/checkins \
  -H 'Content-Type: application/json' -d '{"value":3,"mood":"🙂"}'
curl -s -b jar "localhost:8080/api/v1/projects/$ID/stats?weeks=26"

# Excuse a day without breaking the streak.
curl -s -b jar -X POST localhost:8080/api/v1/projects/$ID/skips \
  -H 'Content-Type: application/json' -d '{"date":"2026-11-20","reason":"business trip"}'

# Conditional write: send back the ETag you read.
curl -s -b jar -X PATCH localhost:8080/api/v1/projects/$ID \
  -H 'Content-Type: application/json' -H 'If-Match: "v1"' \
  -d '{"target":10}'
```
