# Phase 6 — Admin

Service: `services/admin/`. See `docs/modules/admin/README.md`.

The read model and backoffice. Does not own domain data — reads from all other services.
Owns: reports, comparable sales, network dashboard, enquiry forms, audit log search.

## 6.1 DB migrations

```sql
-- Report rollups (materialised, refreshed hourly by background job)
sales_rollups (
  id, branch_id, franchise_id, trading_date DATE,
  order_count INTEGER, fulfilled_count INTEGER, cancelled_count INTEGER,
  gross_revenue_cents INTEGER, net_revenue_cents INTEGER,
  delivery_revenue_cents INTEGER, tax_cents INTEGER,
  avg_order_value_cents INTEGER, currency VARCHAR(3),
  computed_at TIMESTAMPTZ
)

-- Comparable sales computation cache
-- A branch is "comparable" if it was trading in both the current and comparison period
comparable_branches_cache (
  id, period_start DATE, period_end DATE,
  comparison_start DATE, comparison_end DATE,
  comparable_branch_ids UUID[],
  non_comparable_branch_ids UUID[],
  computed_at TIMESTAMPTZ
)

-- Enquiry forms (custom order, franchise, contact)
enquiries (
  id UUID PRIMARY KEY,
  type VARCHAR NOT NULL,       -- 'custom_order' | 'franchise' | 'contact'
  submitter_name VARCHAR,
  submitter_email VARCHAR,
  submitter_phone VARCHAR,
  data JSONB NOT NULL,         -- form-specific fields
  status VARCHAR DEFAULT 'new', -- 'new' | 'read' | 'responded' | 'closed'
  assigned_to UUID,
  responded_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ
)

-- Content pages (FAQ, returns, privacy, terms)
content_pages (
  id UUID PRIMARY KEY,
  slug VARCHAR UNIQUE NOT NULL,  -- 'faq' | 'returns' | 'privacy' | 'terms'
  title VARCHAR NOT NULL,
  content TEXT NOT NULL,          -- markdown
  last_edited_by UUID,
  published_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ, updated_at TIMESTAMPTZ
)
```

## 6.2 Reports API

### Network dashboard (super admin)

```
GET /v1/admin/dashboard/network
→ {
    total_revenue_today_cents,
    total_orders_today,
    active_branches,
    suspended_branches,
    top_branches: [{branch_id, name, revenue_cents, order_count}],
    network_trend: [{date, revenue_cents, order_count}]  -- last 30 days
  }
```

### Comparable sales report (SA-16)

This report answers: "Which locations are truly growing versus those that just opened?"

Definition: a branch is **comparable** if it was trading in BOTH the current period AND
the comparison period. Branches that opened, closed, or were suspended mid-period are
excluded from the comparable figure and reported separately.

```
GET /v1/admin/reports/comparable-sales
  ?current_start=2026-08-01&current_end=2026-08-31
  &comparison_start=2026-07-01&comparison_end=2026-07-31
→ {
    comparable: {
      revenue_current_cents, revenue_comparison_cents, change_pct,
      order_count_current, order_count_comparison, change_pct,
      branches: [{branch_id, name, current, comparison, delta_pct}]
    },
    non_comparable: {
      new_branches: [{branch_id, name, revenue_cents, open_date}],
      closed_branches: [{branch_id, name, revenue_cents, close_date}]
    }
  }
```

### Branch reports

```
GET /v1/admin/reports/branch/{id}/sales
  ?from=2026-08-01&to=2026-08-31
→ daily sales, order counts, top products, cancellations, average order value

GET /v1/admin/reports/branch/{id}/orders
  ?from=X&to=Y&status=Z
→ order list for the period (paginated)

GET /v1/admin/reports/network/branch-comparison
→ side-by-side table: all branches, current period, sortable by revenue/orders/AOV
```

### Tax export

```
GET /v1/admin/reports/tax-export
  ?from=2026-08-01&to=2026-08-31&format=csv
→ CSV with order-level tax breakdown for accounting
```

## 6.3 Audit log search (SA-15)

```
GET /v1/admin/audit-log
  ?franchise_id=X&branch_id=Y&actor_id=Z
  &action=order.refunded
  &from=2026-08-01&to=2026-08-31
  (paginated, 50 per page)
→ [{id, timestamp, actor, action, resource, before, after, ip, device}]
```

## 6.4 Enquiry forms API

**Storefront submission (public):**
```
POST /v1/enquiries/custom-order
POST /v1/enquiries/franchise
POST /v1/enquiries/contact
```

**Admin management:**
```
GET   /v1/admin/enquiries?type=X&status=Y   → list
GET   /v1/admin/enquiries/{id}               → detail
PATCH /v1/admin/enquiries/{id}/status
PATCH /v1/admin/enquiries/{id}/assign
```

## 6.5 Content management API

```
GET   /v1/content/{slug}                    → public page content (storefront reads this)
GET   /v1/admin/content                     → all pages list
GET   /v1/admin/content/{slug}
PUT   /v1/admin/content/{slug}              → full replace (super admin only, brand-locked)
```

## 6.6 Discount codes API (admin-owned)

```
GET    /v1/admin/discount-codes
POST   /v1/admin/discount-codes
GET    /v1/admin/discount-codes/{id}
PATCH  /v1/admin/discount-codes/{id}/deactivate
GET    /v1/admin/discount-codes/{id}/usage-report
```

Discount code types: `fixed_amount`, `percentage`, `free_delivery`.
Constraints: min order, max uses, valid dates, first-time only flag.

## 6.7 Background jobs

Hourly: refresh `sales_rollups` from `orders` data.
Daily: recompute `comparable_branches_cache` for current-month vs prior-month.
On demand: export jobs (large CSV/PDF exports run async, client polls for completion).

## 6.8 Swagger + tests

`services/admin/api/openapi.yaml`. `postman/admin.postman_collection.json`.
Unit tests: comparable sales logic — branches in both periods included; branches open
mid-period excluded from comparable figure and included in non-comparable.

## Previous / Next

← [Phase 5 — Payments](phase5-payments.md) · [Phase 7 — Realtime](phase7-realtime.md) →
