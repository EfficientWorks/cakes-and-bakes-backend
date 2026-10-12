# RBAC — Permission matrix

## Permission keys (action-level)

```
orders.read              — view orders (own branch scope)
orders.read.network      — view orders across all branches (super admin / finance)
orders.confirm           — manually confirm an order
orders.amend             — amend an order within the amendment window
orders.cancel            — cancel an order
orders.refund.limited    — issue refund up to numeric_limits.max_refund_cents
orders.refund.unlimited  — issue any refund value

catalog.read             — view catalogue
catalog.manage           — create/edit/delete products (super admin only)
catalog.availability     — enable/disable item availability (branch manager+)

franchise.read           — view franchise and branch directory
franchise.create         — create a new franchise (super admin)
franchise.suspend        — suspend a franchise or branch (super admin)
franchise.manage.own     — manage own franchise config (franchise owner)

branch.config            — edit branch hours, capacity, delivery zone
branch.emergency_close   — close all today's slots immediately
branch.staff             — manage staff accounts at own branch

reports.own_branch       — view own branch sales
reports.network          — view consolidated network dashboard
reports.comparable       — view comparable sales report
reports.export           — export CSV/PDF

customers.read           — view customer contact details
customers.export         — export customer data (PII)

audit.read               — search audit log
content.manage           — edit CMS pages (super admin only)
enquiries.manage         — manage enquiry forms

kds.items                — mark items in-progress / done
pos.orders               — create orders on POS
pos.cash                 — record cash transactions
pos.shift                — open/close shift, day close
pos.terminal.login       — login via PIN (not email)
```

## Role defaults

| Permission | super_admin | finance | franchise_owner | franchise_viewer | branch_manager | counter_staff | kitchen_staff |
|---|:---:|:---:|:---:|:---:|:---:|:---:|:---:|
| orders.read | ● | ● | ● | ● | ● | ● | |
| orders.read.network | ● | ● | | | | | |
| orders.confirm | ● | | ● | | ● | | |
| orders.amend | ● | | ● | | ● | ● | |
| orders.cancel | ● | | ● | | ● | | |
| orders.refund.limited | ● | ● | ● | | ● | ● | |
| orders.refund.unlimited | ● | ● | ● | | ● | | |
| catalog.read | ● | ● | ● | ● | ● | ● | ● |
| catalog.manage | ● | | | | | | |
| catalog.availability | ● | | ● | | ● | | |
| franchise.read | ● | ● | own | own | | | |
| franchise.create | ● | | | | | | |
| franchise.suspend | ● | | | | | | |
| franchise.manage.own | ● | | ● | | | | |
| branch.config | ● | | ● | | ● | | |
| branch.emergency_close | ● | | ● | | ● | | |
| branch.staff | ● | | ● | | ● | | |
| reports.own_branch | ● | ● | ● | ● | ● | ● | |
| reports.network | ● | ● | | | | | |
| reports.comparable | ● | ● | | | | | |
| reports.export | ● | ● | ● | | | | |
| customers.read | ● | | ● | | ● | ● | |
| customers.export | ● | | ● | | | | |
| audit.read | ● | | | | | | |
| content.manage | ● | | | | | | |
| enquiries.manage | ● | | | | | | |
| kds.items | ● | | ● | | ● | ● | ● |
| pos.orders | ● | | | | ● | ● | |
| pos.cash | ● | | | | ● | ● | |
| pos.shift | ● | | | | ● | ● | |
| pos.terminal.login | | | | | ● | ● | ● |

## Numeric limits (per user, configurable)

| Limit key | Default counter_staff | Default branch_manager | Default franchise_owner |
|---|---|---|---|
| `max_refund_cents` | 1000 (=$10) | 5000 (=$50) | unlimited |
| `max_discount_pct` | 0 | 20 | 50 |
| `void_window_minutes` | 0 | 30 | 60 |

These are seeded defaults. A franchise owner can adjust limits for their own staff.
A super admin can adjust any limit.

## Permission resolution algorithm

```
1. If realm == support → grant everything, return.
2. If no franchise membership for this request → 403.
3. Load role defaults from role_permissions.
4. Apply user-level overrides from permission_overrides (grant/deny, check expiry).
5. Filter by branch scope from JWT claims.
6. For numeric-limit actions, compare request value against numeric_limits.
7. Return decision.
```

Permissions are embedded in the JWT and re-resolved on every refresh.
The server re-checks on every mutation — JWT claims are trusted but mutations
always verify server-side (defence in depth, prevents stale token abuse).
