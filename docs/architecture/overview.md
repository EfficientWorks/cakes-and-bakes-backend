# Architecture overview

`bb-server` is a multi-tenant, multi-branch franchise ordering platform backend. It is a
Go monorepo of small services behind a BFF gateway, following the same architecture
pattern as `emzo-restaurant-server` but scoped to a gifting-commerce domain with a
3-level Brand → Franchise → Branch hierarchy.

## System shape

```
Clients                    Services (ConnectRPC)
──────────────────         ──────────────────────────────────────────
Customer website    ──►    gateway BFF  ──►  identity
Admin panels        ──►    (REST/JSON        franchise
POS app (Flutter)  ──►     /v1 + SSE)  ──►  catalog
KDS app (Flutter)  ──►                 ──►  orders
                                        ──►  scheduling
                                        ──►  payments
                                        ──►  admin
                                    ──SSE►   realtime

Services write domain events to outbox table (same Postgres tx as state change)
In-process relay goroutine publishes outbox events → realtime → SSE → clients
```

## The 9 services

See `.claude/context/service-map.md` for the full table.

## Data and persistence

- **One Postgres server** — `bb_global` (super-admin/registry data) + `bb_us` (regional
  operational data). Adding `bb_eu` or `bb_uk` later is one line.
- `pgx` + `sqlc` (typed generated queries) + `goose` (migrations).
- Services with global + regional data (`identity`, `franchise`) keep two migration
  directories: `db/migrations/global/` and `db/migrations/regional/`.
- Purely regional services (`catalog`, `orders`, `scheduling`, `payments`, `admin`) keep
  `db/migrations/` only.
- **Money**: integer minor units + `currency` column. **Time**: `timestamptz` + branch
  `timezone` for local display.

## Domain events (outbox)

- State change + outbox row in one `pkg/db.WithTx` call.
- In-process relay goroutine polls outbox and dispatches.
- `events.NoopPublisher` until `orders` wires it in Phase 4.
- Consumers must be idempotent (outbox guarantees at-least-once).

## Realtime (SSE)

- `realtime` service fans out per-branch SSE channels.
- Initial impl: in-memory hub — correct for single Railway instance.
- Scale trigger: add Redis pub/sub backplane when more than one `realtime` instance.

## Authentication

- **Two consumer realms**: `customer` (storefront account) and `staff` (admin/POS/KDS),
  plus `support` (super admin, region-less).
- EdDSA JWTs, short-lived access tokens, rotating refresh tokens hashed in Postgres.
- argon2id for passwords (customers) and PINs (staff terminal login).
- JWKS at `/.well-known/jwks.json` — other services verify tokens without calling back.

## Performance targets

| Metric | Target |
|---|---|
| API p95 | < 300ms region-local |
| Availability change → storefront | < 30 seconds |
| Branch suspension → storefront | < 30 seconds |
| SSE push (KDS, POS order arrival) | < 2 seconds end-to-end |
| Customer site LCP | < 2.5s (Core Web Vitals 75th pct) |

## Deployment

Railway: one Postgres plugin, one service per Railway app service.
Vercel: Next.js customer site + admin panels.
Cloudflare: CDN, WAF, SSL.
See `docs/deployment/deployment.md`.
