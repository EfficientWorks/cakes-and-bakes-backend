# Phase 9 — Gateway

Service: `services/gateway/`. The BFF (Backend for Frontend).

Terminates all client traffic, authenticates, rate-limits, and routes to internal services
via ConnectRPC. This is the only service with a public internet-facing port.

## 9.1 Responsibilities

1. **Auth middleware** — validate JWT, set tenant scope via `pkg/tenant`.
2. **Rate limiting** — per IP for public endpoints, per user for authenticated.
3. **Request routing** — proxy REST requests to the correct internal service.
4. **SSE proxy** — forward SSE connections to `realtime` service.
5. **CORS** — allow `bakedbouquet.com`, `admin.bakedbouquet.com`.
6. **Health check** — `GET /health` (used by Railway + Cloudflare).

## 9.2 Route table

All routes under `/v1`. The gateway strips `/v1` and proxies to the internal service.

| Route prefix | Internal service |
|---|---|
| `/v1/auth/*` | `identity` |
| `/v1/customer/*` | `identity` |
| `/v1/admin/franchises/*` | `franchise` |
| `/v1/admin/branches/*` | `franchise` |
| `/v1/franchise/*` | `franchise` |
| `/v1/branch/*` | `franchise` |
| `/v1/catalog/*` | `catalog` |
| `/v1/admin/catalog/*` | `catalog` |
| `/v1/orders/*` | `orders` |
| `/v1/kds/*` | `orders` |
| `/v1/pos/*` | `orders` |
| `/v1/scheduling/*` | `scheduling` |
| `/v1/payments/*` | `payments` |
| `/v1/webhooks/stripe` | `payments` (bypasses auth — uses Stripe signature) |
| `/v1/admin/reports/*` | `admin` |
| `/v1/admin/dashboard/*` | `admin` |
| `/v1/admin/audit-log/*` | `admin` |
| `/v1/enquiries/*` | `admin` (public — no auth) |
| `/v1/admin/enquiries/*` | `admin` |
| `/v1/content/*` | `admin` (public reads) |
| `/v1/admin/content/*` | `admin` |
| `/v1/realtime/events` | `realtime` (SSE — upgraded to streaming) |
| `/v1/track/*` | `orders` (public — no auth, secure token) |
| `/.well-known/jwks.json` | `identity` (public — no auth) |

## 9.3 Auth middleware

For all authenticated routes:
1. Extract `Authorization: Bearer <token>`.
2. Verify JWT signature against `identity` JWKS (cached with 5-minute TTL).
3. Set `pkg/tenant.Scope` in request context: `{Realm, Region, BrandID, FranchiseID, BranchID, Role, Permissions}`.
4. Forward to internal service with tenant scope in headers.

Public routes (no JWT required): `/v1/auth/*`, `/v1/enquiries/*`, `/v1/content/*`,
`/v1/track/*`, `/.well-known/jwks.json`, `/v1/webhooks/stripe`.

## 9.4 Rate limiting

```
Per IP (unauthenticated):
  - /v1/auth/customer/login → 10 req/min
  - /v1/auth/customer/register → 5 req/min
  - /v1/enquiries/* → 20 req/hour
  - All other public → 100 req/min

Per user (authenticated):
  - /v1/orders (POST) → 10 req/min
  - All other → 500 req/min
```

Implemented via in-memory sliding window (no Redis at launch). Rate limit state is
per-instance — acceptable at single-instance Railway deployment.

## 9.5 No DB

Gateway has no database. All state is derived from JWT claims and routed to services.

## 9.6 Swagger

Gateway publishes the consolidated OpenAPI spec by combining specs from all services.
`GET /v1/openapi.json` → merged spec.

## Previous / Next

← [Phase 8 — POS + KDS](phase8-pos-kds.md) ← All phases complete.
