# HTTP API

Base URL: `http://localhost:8080/api/v1`. Requests and responses are JSON.
Every data route — reads included — requires the administrator session (cookie
or `Authorization: Bearer`). Only the auth door and the operational probes are
public; everything else answers `401` without a session.

## Endpoints

| Method   | Path                                 | Access  | Purpose                                                    |
| -------- | ------------------------------------ | ------- | ---------------------------------------------------------- |
| `GET`    | `/health`                            | public  | Liveness. Process state only; never touches a dependency    |
| `GET`    | `/health/ready`                      | public  | Readiness. `503` when a dependency check fails              |
| `GET`    | `/version`                           | public  | Build metadata, environment, uptime                         |
| `POST`   | `/auth/login`                        | public  | Sign in; sets the session cookie. Rate-limited per IP       |
| `POST`   | `/auth/logout`                       | public  | Clear the session                                           |
| `GET`    | `/auth/me`                           | public  | The request's role and username                             |
| `GET`    | `/categories`                        | admin   | All categories                                              |
| `POST`   | `/categories`                        | admin   | Create                                                      |
| `PATCH`  | `/categories/{id}`                   | admin   | Replace name/icon/color                                     |
| `DELETE` | `/categories/{id}`                   | admin   | Delete; its projects become uncategorized                   |
| `GET`    | `/projects`                          | admin   | List with current status. `date`, `archived`, `category`, `q` |
| `POST`   | `/projects`                          | admin   | Create. `201` with `Location` and `ETag`                    |
| `GET`    | `/projects/{id}`                     | admin   | Read. Honours `If-None-Match` → `304`                       |
| `PATCH`  | `/projects/{id}`                     | admin   | Partial update. `If-Match` enforced in SQL → `412`          |
| `DELETE` | `/projects/{id}`                     | admin   | Delete; check-ins and skips cascade                         |
| `GET`    | `/projects/{id}/checkins`            | admin   | History. `from`, `to`, `limit`                              |
| `POST`   | `/projects/{id}/checkins`            | admin   | Check in / backfill (`date` in the past); optional `note`, `photo` |
| `GET`    | `/checkins/{id}/photo`               | admin   | Proof photo bytes; `404` when the record has none           |
| `DELETE` | `/checkins/{id}`                     | admin   | Undo one check-in                                           |
| `GET`    | `/projects/{id}/skips`               | admin   | Excused days                                                |
| `POST`   | `/projects/{id}/skips`               | admin   | Excuse a day; duplicate → `409`                             |
| `DELETE` | `/projects/{id}/skips/{date}`        | admin   | Un-excuse a day                                             |
| `GET`    | `/projects/{id}/stats`               | admin   | Summary, totals, heatmap (`weeks`), trend, recent, skips    |
| `GET`    | `/stats/overview`                    | admin   | Cross-project dashboard (good progress / bad lapses apart)  |
| `GET`    | `/export`                            | admin   | Full JSON backup, offered as a download                     |
| `POST`   | `/import`                            | admin   | Restore a backup additively by id (32 MiB hard cap)         |
| `POST`   | `/admin/reset`                       | admin   | Password-confirmed wipe; deletes SQLite and re-seeds demo if configured |
| `GET`    | `/debug/pprof/*`                     | public  | Only with `SPLASH_ENABLE_PPROF=true`                        |

The gate is applied once at route registration (`api.guard`), so reads and
writes are protected by construction. There is no anonymous data access and no
per-project visibility flag.

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

## Conventions worth knowing

- **Supplements.** Every check-in accepts a free-text `note` (max 200 chars). A
  project may set `notePrompt` (max 40 chars) to label that field in the record
  sheet, e.g. "什么时候睡的？睡了多久？"; the prompt is configuration, the note is
  stored per check-in and returned by history reads.
- **Photos.** A check-in may carry `photo`, a `data:image/(jpeg|png|webp);base64,…`
  URL capped at 400 KB. Read responses never include the payload: they carry
  `hasPhoto`, and the bytes live at `GET /checkins/{id}/photo`. Exports include
  `photo`, which is why `/import` uses the 32 MiB request ceiling rather than the
  configured default body limit.
- **Pinned.** `pinned` is a project boolean; the UI floats pinned projects to the
  top of Today. Patch it like any other field (`PATCH {"pinned":true}`).
- **Links.** Every project carries `prerequisites` and `postrequisites` (project
  ids). Both are same-day and cycle-free across the union of directions; one
  target may not be both a prerequisite and a postrequisite of the same project.
  Postrequisites are rejected on bad habits. A completed source reports
  `pendingPosts` today and `brokenPosts` once its completion day has closed; the
  stats response also carries `brokenCommitments` (`{date,targetId}`),
  `totals.postReqs` / `postViolations` / `commitmentRate`, and each check-in
  carries `postUnmet` / `postPending`.
- **Reset.** `POST /admin/reset` with `{"password":"…"}` verifies the configured
  administrator password again (otherwise `422` with a `password` field error),
  closes SQLite, deletes the database file and its sidecars, recreates the
  schema, and re-seeds the demo only when `SPLASH_SEED_DEMO=true`. It returns
  `{"reset":true,"seededProjects":N}`. The route holds an exclusive lock: other
  data requests wait for it and in-flight requests finish first. There is no
  undo; export first.

## Example

```bash
curl -s localhost:8080/api/v1/health

# Sign in; the session rides in an HttpOnly cookie.
curl -s -c jar -X POST localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"splash123"}'

# A good habit: 8 cups of water a day.
curl -s -b jar -X POST localhost:8080/api/v1/projects \
  -H 'Content-Type: application/json' \
  -d '{"name":"Water","icon":"💧","color":"blue","nature":"good","schedule":"daily","target":8,"unit":"cups","startDate":"2026-11-01"}'

# A bad habit: staying up late. nature=bad forces check mode; a check-in is a
# lapse, and the streak counts nights survived clean.
curl -s -b jar -X POST localhost:8080/api/v1/projects \
  -H 'Content-Type: application/json' \
  -d '{"name":"Late nights","icon":"🦉","color":"indigo","nature":"bad","schedule":"daily","startDate":"2026-11-01"}'

# A habit with a custom supplement prompt.
curl -s -b jar -X POST localhost:8080/api/v1/projects \
  -H 'Content-Type: application/json' \
  -d '{"name":"Sleep","icon":"😴","color":"violet","mode":"check","schedule":"daily","notePrompt":"什么时候睡的？睡了多久？","startDate":"2026-11-01"}'

# Record it with the free-text supplement (a photo data URL can ride along).
curl -s -b jar -X POST localhost:8080/api/v1/projects/$SLEEP_ID/checkins \
  -H 'Content-Type: application/json' \
  -d '{"value":1,"note":"22:40 入睡，睡了 7 小时"}'

# Pin it to the top of Today.
curl -s -b jar -X PATCH localhost:8080/api/v1/projects/$SLEEP_ID \
  -H 'Content-Type: application/json' -H 'If-Match: "v1"' -d '{"pinned":true}'

# Same-day link: today's run commits tonight's sleep. If the sleep habit is not
# done on the run's completion day, the run reports an unfulfilled commitment.
curl -s -b jar -X PATCH localhost:8080/api/v1/projects/$RUN_ID \
  -H 'Content-Type: application/json' -H 'If-Match: "v1"' \
  -d '{"postrequisites":["'$SLEEP_ID'"]}'

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

# DESTRUCTIVE: wipe SQLite and recreate it. The password is the configured
# administrator password, re-entered even though the session is already admin.
curl -s -b jar -X POST localhost:8080/api/v1/admin/reset \
  -H 'Content-Type: application/json' -d '{"password":"splash123"}'
```
