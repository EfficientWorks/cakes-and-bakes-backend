# ADR-0002 — 3-Level Brand → Franchise → Branch model

## Status

Accepted

## Context

emzo-restaurant uses a 2-level Business → Outlet model. Baked Bouquet requires a 3-level
hierarchy because:
- The brand (Baked Bouquet HQ) controls the master catalogue and brand governance.
- A franchisee owns one or more branches and needs operational control + their own reporting.
- A branch is a physical kitchen/store with its own timezone, delivery zone, and hours.
- Company-owned locations must behave identically to franchised ones (same code path).

## Decision

Every operational record carries `franchise_id` AND `branch_id`, written at creation and
never changed. The brand is implicit (there is only one brand at launch).

A company-owned location is modelled as a franchise whose owner is the brand. This
removes an entire class of edge cases.

## Consequences

- JWT claims for staff must carry both `franchise_id` and `branch_id`.
- `pkg/tenant.Scope` adds `FranchiseID` and `BranchID` fields.
- RLS policies filter on both fields.
- The governance matrix (brand-locked vs franchise-editable vs branch-editable) is enforced
  server-side on every write.
- Reporting can aggregate at franchise level (all of a franchisee's branches) or network
  level (all franchises), without joins or data denormalisation.
