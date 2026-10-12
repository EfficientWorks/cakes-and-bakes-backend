# Deploying to Railway

The backend is a modular monolith: one Go binary (`cmd/server`) that migrates on
startup and serves everything. On Railway that means **three services in one
project** — the API, Postgres and Redis.

| Railway service | What it is |
|---|---|
| `bb-server` | This repo, built from `Dockerfile` |
| `Postgres` | Railway Postgres plugin — supplies `DATABASE_URL` |
| `Redis` | Railway Redis plugin — supplies `REDIS_URL` |

Redis is optional. `ratelimit.New` returns a no-op limiter when Redis is
unreachable, so the API boots without it — but login and OTP-resend throttling
are then off, which you do not want in production.

## What is in the repo for this

- `Dockerfile` — multi-stage build, distroless-ish Alpine runtime, non-root.
  Ships `db/migrations` next to the binary because `db.Migrate` reads them from
  disk, and installs `tzdata` because branches carry IANA timezones.
- `.dockerignore` — keeps `.git`, `.env`, docs and Postman out of the image.
- `railway.json` — pins the Dockerfile builder and wires the `/health`
  healthcheck plus an on-failure restart policy.

## First deploy

```bash
railway login
railway init            # or: railway link   (to an existing project)
railway add --database postgres
railway add --database redis
```

Set the variables (below), then:

```bash
railway up
railway domain          # allocate the public URL
```

Migrations run automatically on every boot via `db.Migrate` in `cmd/server`, so
there is no separate release step.

## Variables

Set these on the **API service**. `${{Postgres.DATABASE_URL}}` and
`${{Redis.REDIS_URL}}` are Railway reference variables — paste them literally
and Railway resolves them to the private-network addresses.

### Required

| Variable | Value |
|---|---|
| `APP_ENV` | `production` (or `staging`) |
| `PORT` | `8080` |
| `DATABASE_URL` | `${{Postgres.DATABASE_URL}}` |
| `REDIS_URL` | `${{Redis.REDIS_URL}}` |
| `CUSTOMER_JWT_SECRET` | a fresh random secret |
| `STAFF_JWT_SECRET` | a *different* fresh random secret |
| `CORS_ALLOWED_ORIGINS` | your storefront + admin origins, comma-separated |

`Config.Validate` refuses to start outside `APP_ENV=local` if either JWT secret
is missing or still the dev default, if the two match each other, or if
`CORS_ALLOWED_ORIGINS` contains `*` — responses carry credentials. A failed
deploy with `configuration invalid for APP_ENV=production` in the logs is that
check doing its job.

Generate the secrets with:

```bash
openssl rand -base64 48
```

### Integrations — unset means degraded, not broken

| Variable | Unset behaviour |
|---|---|
| `STRIPE_SECRET_KEY` | orders capture through a no-op processor; no real money moves |
| `STRIPE_WEBHOOK_SECRET` | `/v1/webhooks/stripe` rejects every event |
| `POSTMARK_SERVER_TOKEN` | emails are logged, not sent |
| `POSTMARK_FROM_EMAIL` | defaults to `orders@bakedbouquet.com` |
| `GOOGLE_MAPS_API_KEY` | nearest-branch distance ranking disabled |
| `R2_*` | media uploads unconfigured |
| `SENTRY_DSN` | no error reporting |

Each of these logs a `WARN` at startup naming what was skipped — read the deploy
logs after the first boot and confirm the list matches what you meant.

### Token lifetimes

`CUSTOMER_ACCESS_TTL` (1h), `CUSTOMER_REFRESH_TTL` (720h), `STAFF_ACCESS_TTL`
(8h), `STAFF_REFRESH_TTL` (168h) all have production-sane defaults. Set them
only to override.

## Stripe webhook

Once the domain exists, point the Stripe endpoint at:

```
https://<your-domain>/v1/webhooks/stripe
```

and put its signing secret in `STRIPE_WEBHOOK_SECRET`.

## Swagger

`MountSwagger` skips registration entirely when `APP_ENV=production` — the spec
enumerates every admin route. On a staging service set `APP_ENV=staging` and
`/docs/index.html` is served.

## Seeding

`cmd/seed` creates a super admin (`admin@bakedbouquet.com` / `ChangeMe123!`),
one franchise and one branch. Its own doc comment says **never run this against
a production database** — those credentials are hardcoded and public in this
repo. Treat it as a staging-only bootstrap.

```bash
railway run --service bb-server -- go run ./cmd/seed
```

`railway run` injects the service's variables into your local shell and rewrites
`*.railway.internal` hosts to the public proxy, so this runs on your machine
against the Railway database — the Postgres service needs its TCP proxy enabled
for that. The seeder is deliberately not in the runtime image.

For a real first admin in production, seed a staging environment and create the
production admin through the admin API, or change the constants in
`cmd/seed/main.go` before running.

## Health

`GET /health` returns `{"status":"ok"}` and touches nothing — it is a liveness
check, not a readiness check. It answers 200 even if Postgres has gone away, so
do not read a green healthcheck as "the database is fine".

## Troubleshooting the first boot

**`configuration invalid for APP_ENV=production`** — `Config.Validate` rejected
the variables. The message names every problem at once; fix them all and
redeploy.

**`migration failed` / `ping database` with a DNS or connection-refused error**
— Railway's private network can take a few seconds to come up on a cold start,
and `cmd/server` migrates before it listens. The `ON_FAILURE` restart policy in
`railway.json` retries, so this usually self-resolves within a restart or two.
If it persists, confirm `DATABASE_URL` is the `${{Postgres.DATABASE_URL}}`
reference and not a stale literal.

**Healthcheck never turns green** — the process is not listening on `PORT`.
Check that `PORT` is set on the service and matches nothing hardcoded.

**Timezone errors on branch creation** (`unknown time zone America/New_York`) —
the runtime image lost `tzdata`. It is installed in the `Dockerfile`; if you
swap the base image, keep it.

**`dockerfile invalid: flag '--mount=type=cache,...' is missing an id argument`**
— Railway's Metal builder requires every BuildKit cache mount to carry an `id`.
The `Dockerfile` deliberately uses no cache mounts: an `id` that actually shares
a cache has to encode the Railway service id (`id=s/<service-id>-<path>`), which
would pin the repo to one service. Copying `go.mod`/`go.sum` before the source
gives the same dependency-layer caching and stays portable.
