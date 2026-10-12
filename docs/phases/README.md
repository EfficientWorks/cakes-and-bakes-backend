# Backend build sequence

Build order for `bb-server`, phase by phase. Each phase file lists deliverables and
where they live. Requirements for each module are supplied when that phase starts.

| Phase | Service | Contents | Release target |
|---|---|---|---|
| [0 — Foundation](phase0-foundation.md) | Shared `pkg/`, docker-compose, Makefile | Scaffold | Pre-R1 |
| [1 — Identity](phase1-identity.md) | `identity` — customer auth + staff auth + RBAC + JWKS | Core auth | R1 |
| [2 — Franchise](phase2-franchise.md) | `franchise` — Brand/Franchise/Branch CRUD, delivery zones, governance | Hierarchy | R1 |
| [3 — Catalog](phase3-catalog.md) | `catalog` — products, availability cascade, occasion tags | Catalogue | R1 |
| [4 — Orders + Scheduling](phase4-orders.md) | `orders` + `scheduling` — routing, lifecycle, slots, capacity | Order engine | R1 |
| [5 — Payments](phase5-payments.md) | `payments` — Stripe online, refunds, Stripe Terminal | Payments | R1 + R2 |
| [6 — Admin](phase6-admin.md) | `admin` — reports, comparable sales, network dashboard, enquiry forms | Backoffice | R1 |
| [7 — Realtime](phase7-realtime.md) | `realtime` — SSE hub, per-branch channels | Live push | R2 |
| [8 — POS + KDS](phase8-pos-kds.md) | `orders` + `scheduling` + `payments` extensions for POS/KDS flows | POS/KDS | R2 |
| [9 — Gateway](phase9-gateway.md) | `gateway` — BFF, auth middleware, rate-limit, SSE proxy | API surface | R1 |

## Release map

```
R1 — 28 Aug 2026
  Phases: 0, 1, 2, 3, 4 (storefront-facing), 5 (Stripe online), 6, 9
  Outcome: Customer can order online end-to-end. Admin sees and closes orders.

R2 — 9 Sep 2026
  Phases: 4 (POS extensions), 5 (Stripe Terminal), 7, 8
  Outcome: POS live at counter. KDS receives orders in real time.

R3 — End Sep 2026
  Data migration (Square + Weebly), UAT, full go-live.
```

## Cross-cutting rules (apply to every phase)

- **Franchise-first.** Every regional table carries `franchise_id` + `branch_id`.
- **Outbox from Phase 4 onward.** `NoopPublisher` until `orders` wires it.
- **No Redis, no cloud IaC** — Railway is the deployment target.
- **Every phase ends with**: unit tests + Swagger/OpenAPI + Postman collection.
- **Idempotency keys** on order creation and payment — duplicates are safe.

## See also

- `.claude/context/service-map.md` — which service owns what
- `docs/architecture/overview.md` — system shape
- `docs/decisions/` — ADRs explaining every non-obvious choice
