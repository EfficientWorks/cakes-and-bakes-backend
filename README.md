# emzo Bakehouse — backend

The API for emzo Bakehouse, a multi-store ordering product for bakeries: customer
website, admin console, POS till and kitchen display. Go, chi, pgx, goose,
Postgres 18, Redis. Conventions are in [CLAUDE.md](CLAUDE.md).

## Quick start

```bash
cp .env.example .env
make dev-up      # Postgres on 5500, Redis on 6390 (infra/docker/docker-compose.yml)
make run         # migrates itself on startup
make seed        # local super admin (admin@example.com) and one UK store
```

- API base URL: `http://localhost:<PORT>/v1` (PORT from .env)
- Swagger UI: `http://localhost:<PORT>/docs/index.html`
- Postman collection: [`postman/`](postman/)

`make help` lists every target; `make check` is the gate before pushing.

## Shape

A **modular monolith**. Each service under `services/` keeps its own
`api → domain → store` layering and its own private `internal/` tree, but
they compile into one binary and are wired together in
[`cmd/server/routes.go`](cmd/server/routes.go).

```
pkg/                      shared, importable by every service
├── apperr/               error type + single-sourced UPPER_SNAKE codes
├── audit/                append-only audit log writer
├── auth/                 JWT issue/verify, argon2id, middleware
├── config/               env loading + startup validation
├── db/                   pgxpool, goose migrations, WithTx
├── email/                email (SendGrid / SMTP, logging fallback)
├── events/               transactional outbox + in-process relay
├── flags/                FEATURE_* feature switches
├── geo/                  geocoding + distance (noop fallback)
├── money/                minor units -> text, per currency
├── httpx/                router, JSON envelope, error writer, Swagger UI
├── obs/                  slog structured logging
├── ratelimit/            Redis fixed-window limiter (no-op fallback)
├── strx/ timex/          shared null-handling and date helpers
├── tenant/               scope + RBAC permission matrix
└── testutil/             test helpers and integration fixtures

services/<svc>/
├── <svc>api/             published contract — types + interfaces only
├── <svc>.go              composition root: New(...) and Mount*(...)
└── internal/
    ├── api/              HTTP handlers, DTOs, routes
    ├── domain/           business logic
    └── store/            pgx queries, one folder per topic (see CLAUDE.md)
```

Services: `identity`, `franchise`, `catalog`, `orders`, `payments`, `admin`, `enquiries`.

### Why `internal/`

A service's `internal/` tree is importable only from within that service —
the Go compiler enforces it. That is what replaces the process boundary a
separate deployment would have provided: `orders` **cannot** reach into
`franchise`'s store even by accident.

Services talk to each other through the `<svc>api` contract packages
instead. `orders` declares what it needs as narrow interfaces
([`orders.Deps`](services/orders/orders.go)) and `cmd/server` passes in the
implementations:

```go
franchiseSvc := franchise.New(pool, auditWriter, geocoder)
catalogSvc   := catalog.New(pool, auditWriter, franchiseSvc.Branches())
ordersSvc    := orders.New(pool, orders.Deps{
    Branches: franchiseSvc.Branches(),   // franchiseapi.BranchReader
    Pricing:  catalogSvc.Pricing(),      // catalogapi.Pricer
    Payments: paymentsSvc.Processor(),   // paymentsapi.Processor
})
```

Those seams are where an RPC client would go if a service were ever split
out. Nothing else would have to change.

### Scheduling lives inside orders

Reserving slot capacity and creating the order happen in one transaction
(ADR-0006), so scheduling is `orders/internal/…` rather than a service of
its own.

## Conventions

- **Money** is always integer minor units + currency. Never floats.
- **Time** is `timestamptz`; branch-local dates use the branch's `timezone`.
- **Tenancy** is derived from the JWT via `pkg/tenant`, never from the URL
  or body. Middleware checks the permission; the service re-checks scope on
  the resolved resource.
- **Errors** are `*apperr.Error` with a code from `pkg/apperr/codes.go`,
  rendered by `httpx.WriteError`. Internals and PII never reach the client.
- **Every endpoint** needs a unit test, a Swagger annotation, and a Postman
  request.

## Domain events

State changes write an outbox row in the same transaction via
`db.WithTx` + `events.Outbox.Write`. An in-process relay drains it. Delivery
is at-least-once, so **consumers must be idempotent**. The publisher is
`NoopPublisher` until a consumer exists — the rows are still written, so
nothing is lost when one lands.

## Graceful degradation

Missing third-party credentials log a warning instead of failing startup:
no Stripe key → no-op payment processor; no Postmark token → emails are
logged; no Maps key → no distance ranking; no Redis → rate limiting off.

Configuration that would be *unsafe* does fail fast: outside
`APP_ENV=local`, `config.Validate` refuses to start on default or shared JWT
secrets, or a wildcard CORS origin.

## Testing

```bash
make test-unit         # no database needed
make test-integration  # needs DATABASE_URL; tests tagged //go:build integration
```

[`cmd/server/routes_test.go`](cmd/server/routes_test.go) builds the entire
route tree against a nil pool. Four services share `/admin/branches` and chi
panics at registration time on a duplicate mount, so that failure is caught
in CI rather than on boot.

## Regenerating the API docs

```bash
make swagger   # rereads the // @... handler annotations into api/
```

## Documentation

Architecture, conventions, ADRs and per-phase build notes live in `docs/`.

- Entry point: [`CLAUDE.md`](CLAUDE.md) · context: [`.claude/context/`](.claude/context/)
- Architecture: [`docs/architecture/`](docs/architecture/) · conventions: [`docs/development/conventions.md`](docs/development/conventions.md)
- ADRs: [`docs/decisions/`](docs/decisions/) · build phases: [`docs/phases/`](docs/phases/)
- Product requirements: [`docs/requirements/`](docs/requirements/)
