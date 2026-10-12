# emzo Bakehouse backend

The API behind emzo Bakehouse: a multi-store ordering product for bakeries. One Go
binary (a modular monolith) serves the customer website, the admin console, the POS
till and the kitchen display (KDS). UK first; country rules come from configuration,
not code.

Read this file first, then `README.md` for how to run it.

## Layout

```
cmd/server        the API binary: wiring (main.go) and routes (routes.go)
cmd/seed          creates a local super admin and one UK store
services/<svc>    one folder per service (identity, franchise, catalog, orders,
                  payments, admin, enquiries)
  <svc>.go          the service's wiring: New(...) and Mount* functions
  <svc>api/         the service's public contract: types and interfaces other
                    services may import. Nothing else of a service is importable.
  internal/api      HTTP handlers, request/response DTOs, Swagger annotations
  internal/domain   business rules; the only layer that returns apperr codes
  internal/store    SQL, one folder per topic (see below)
pkg/              shared libraries (auth, config, money, email, geo, httpx, …)
db/migrations     goose migrations, applied on server start
api/              generated Swagger (make swagger) and the POS/KDS contract snapshot
tools/apicontract checks that POS/KDS request and response shapes did not change
```

## Store convention

Every `internal/store` is split by topic, one folder (Go package) each:

```
store/
  store.go        facade: embeds every topic Repo, so callers keep s.store.X
  shared/         errors and helpers used by more than one topic
  <topic>/
    repo.go       the topic's Repo, its constructor, and a doc comment saying
                  what the topic is for and any rule a reader must know
    queries.go    SQL that is long or reused, as named constants
    read.go       lookups and lists
    write.go      inserts, updates and deletes
    scan.go       row -> model mapping
    model.go      types that exist only for the store
    helpers.go    small pure functions
```

Short, single-use SQL stays next to the code that scans it. Dependencies between
topics run one way and are passed into `New` (for example `order.New(pool, bills,
returns)`); never import a sibling that imports you.

## Rules that must not be broken

1. **The POS and KDS API contract does not change.** Those apps ship separately.
   `make contract` fails if any `/pos` or `/kds` request or response shape changes;
   fields that no longer carry data stay in the response as null.
2. **Every order carries `branch_id` from creation, and it never changes.** Branch
   resolution happens before payment, never after.
3. **Slot capacity is reserved in the same transaction that creates the order.**
4. **Money is integer minor units (pence) plus a currency code.** Never floats.
   `pkg/money` is the only place money becomes text.
5. **Timestamps are `timestamptz`.** Branch-local dates use the branch timezone.
6. **The server re-authorises every mutation.** JWT claims are only a hint.
7. **Card payments go through Stripe only** (`services/payments`). Square and
   Shipday were removed; do not reintroduce provider-specific columns.

Data isolation is enforced in application code today. There is no row-level security
in the database.

## Before pushing

```
make check   # gofmt, go vet, unit tests, swagger regeneration, POS/KDS contract
```

Every endpoint carries Swagger annotations. Requirement ids (`GOV-01`, `CAT-05`, …) in
comments trace code back to the requirements in `docs/requirements`.
