# Data and persistence

## Database topology

One Postgres server (Railway managed plugin).

```
bb_global          — brand-level registry data, super-admin users
bb_us              — all operational data for US-based branches (launch region)
```

Adding `bb_eu` or `bb_uk` is a one-line addition to the regional pool manager when needed.
There is no separate Postgres instance per region — that is a later scaling step.

## Service → database mapping

| Service | Databases touched |
|---|---|
| `identity` | `bb_global` + `bb_us` |
| `franchise` | `bb_global` + `bb_us` |
| `catalog` | `bb_us` |
| `orders` | `bb_us` |
| `scheduling` | `bb_us` |
| `payments` | `bb_us` |
| `admin` | `bb_us` (+ fan-out reads across regions for network reports) |
| `realtime` | none — in-memory only |
| `gateway` | none — routes to services |

A service only ever touches its own tables. No cross-service foreign keys.
Cross-service data is fetched via ConnectRPC, not by joining tables.

## Migration discipline

```
services/<svc>/db/migrations/global/    — applied to bb_global
services/<svc>/db/migrations/regional/  — applied to bb_us (and future regions)
services/<svc>/db/migrations/           — for purely regional services
```

Migrations use `goose`. Naming: `0001_description.sql`. Every migration is reversible
(Down migration). Destructive changes require a 2-week deprecation cycle:
add nullable → deploy → backfill → deploy → add NOT NULL constraint.

Migration run order matters in a fresh environment:
1. `franchise` global migrations (creates the registry tables `identity` reads)
2. `identity` global migrations
3. `franchise` regional migrations
4. `identity` regional migrations
5. All other services (any order within a region)

## Connection handling

`pgxpool` with per-service pool sizes. `pkg/db.RegionalPools` manages multiple pools
(one per region). Services receive the correct pool from their dependency injection
at startup — they never select which region to connect to at runtime.

## Money

**Always integer minor units (US cents) + `currency VARCHAR(3)`.**

```sql
-- Correct
amount_cents    INTEGER NOT NULL,
currency        VARCHAR(3) NOT NULL DEFAULT 'USD',

-- Never
amount          DECIMAL(10,2),   -- never
price           FLOAT,           -- never
```

Display formatting (e.g. `$88.00`) is the frontend's job.

## Time

All timestamps are `TIMESTAMPTZ` (UTC stored, timezone-aware).

For scheduling and reports, branch-local date is derived in the application layer using
the branch's `timezone` field (a named zone identifier, e.g. `America/New_York`).

Every table that represents an event or state change also stores an explicit **business
date** (the branch-local trading date) alongside the `timestamptz`. This makes day-close
and comparable sales reporting timezone-correct without per-query timezone conversions.

## Row-level security (RLS)

PostgreSQL RLS enforces data isolation at the DB layer for all operational tables.

```sql
-- Every regional table carries at minimum:
branch_id       UUID NOT NULL,
franchise_id    UUID NOT NULL,

-- RLS policy (example for orders):
CREATE POLICY branch_iso ON orders
  USING (branch_id = current_setting('app.current_branch_id')::uuid);
```

The application sets the session variables at connection start via `pkg/tenant`.
A misconfigured application query cannot leak another franchise's data.

## Indexing rules

- Every foreign key has an explicit index (Postgres does not auto-create FK indexes).
- `orders` composite indexes: `(branch_id, scheduled_date)`, `(branch_id, status)`,
  `(franchise_id, created_at)`.
- `audit_log` is partitioned by month from day 1.
- Delivery zone polygons use GIST index (PostGIS).
- Customer search: full-text index on `(first_name || ' ' || last_name)`, email, phone.

## Audit log

Append-only table, partitioned by month. Never deleted, never updated.
Every permission-gated action writes a row capturing: `actor_id`, `action`, `resource_type`,
`resource_id`, `before_value` (JSONB), `after_value` (JSONB), `timestamp`, `branch_id`,
`franchise_id`, `ip_address`, `device_id`.
