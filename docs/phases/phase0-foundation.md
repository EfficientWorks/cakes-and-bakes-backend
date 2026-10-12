# Phase 0 — Foundation

No application logic. Architecture scaffold and tooling every service depends on.

## Deliverables

| # | Item | Where |
|---|---|---|
| 0.1 | `pkg/tenant` — carries `Region`, `Realm`, `BrandID`, `FranchiseID`, `BranchID` | `pkg/tenant/tenant.go` |
| 0.2 | `pkg/db/regional.go` — multi-pool manager (bb_global + per-region) | `pkg/db/regional.go` |
| 0.3 | Auth token claims carry `region`, `realm`, `role`, `franchise_id`, `branch_id` | `pkg/auth/{auth.go,token.go,signer.go}` |
| 0.4 | `docker-compose.yml` — one Postgres server, `bb_global` + `bb_us` databases | `infra/docker/docker-compose.yml` |
| 0.5 | Postgres init script creates both databases | `infra/docker/postgres-init/01-create-databases.sh` |
| 0.6 | `Makefile` — build/run/test/migrate/generate targets | `Makefile` |
| 0.7 | `.env.example` | `.env.example` |
| 0.8 | Full `pkg/` layer ported: `apperr`, `config`, `db`, `auth`, `tenant`, `httpx`, `obs`, `testutil`, `email`, `events`, `flags` | `pkg/` |
| 0.9 | `.golangci.yml`, `buf.yaml`, `buf.gen.yaml`, `.github/workflows/ci.yml` | root |
| 0.10 | Full folder scaffold for all 9 services | `services/` |
| 0.11 | `proto/` folders per service + `proto/events/v1` | `proto/` |
| 0.12 | `postman/` folder + shared local environment file | `postman/` |
| 0.13 | Full `docs/` tree | `docs/` |

## Key difference from emzo-restaurant-server

`pkg/tenant.Scope` now carries `FranchiseID` and `BranchID` in addition to `Region` and
`Realm`. Every service that derives tenant context uses `pkg/tenant` — never parses IDs
from the URL or request body.

JWT claims must carry `franchise_id` and `branch_id` for staff tokens (set at login to
the staff's assigned branch). Super-admin tokens have `franchise_id = nil` and
`branch_id = nil` (they operate network-wide).

## Not done in this pass

- No `main.go` for any service. Application code starts when a phase's requirements arrive.
- No product specs. Domain rules are supplied per phase.

## Next

[Phase 1 — Identity](phase1-identity.md)
