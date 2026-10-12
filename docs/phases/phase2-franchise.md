# Phase 2 — Franchise

Service: `services/franchise/`. See `docs/modules/franchise/README.md`.

Owns the Brand → Franchise → Branch 3-level hierarchy, delivery zone polygons,
governance enforcement, and branch configuration.

## 2.1 Global DB migrations

`services/franchise/db/migrations/global/`:

```sql
-- Registry tables (identity reads these for login routing)
franchise_registry (
  id, slug, legal_entity_name, contact_email, contact_phone,
  territory, commercial_terms, contract_reference,
  status, -- pending | active | suspended | closed
  onboarding_date, created_at, updated_at
)

branch_registry (
  id, franchise_id, name, code UNIQUE, address JSONB,
  geolocation GEOGRAPHY(Point),  -- PostGIS
  timezone VARCHAR(50),           -- e.g. 'America/New_York'
  fulfilment_types VARCHAR[]     -- ['pickup', 'delivery']
  status,  -- pending | active | suspended | closed
  is_central_kitchen BOOLEAN DEFAULT false,
  tax_jurisdiction VARCHAR(5),   -- e.g. 'US-NJ'
  created_at, updated_at
)

-- Registry used by identity for login routing
login_region_map    (domain_hint, region)
branch_region_map   (branch_id, region)
```

## 2.2 Regional DB migrations

`services/franchise/db/migrations/regional/`:

```sql
-- Rich franchise profile (linked to registry by FK)
franchises (
  id,  -- FK to franchise_registry.id in global DB (same UUID)
  primary_contact_name, primary_contact_email, primary_contact_phone,
  billing_address JSONB,
  notes TEXT,
  created_at, updated_at
)

-- Rich branch profile
branches (
  id,  -- FK to branch_registry.id in global DB
  franchise_id,
  display_name, description,
  address JSONB,
  geolocation GEOGRAPHY(Point),
  timezone VARCHAR(50),
  -- Opening hours stored as structured JSONB
  -- {"monday": {"open": "09:00", "close": "18:00"}, ...}
  opening_hours JSONB,
  exception_dates JSONB,   -- blackout dates + reason
  fulfilment_types VARCHAR[],
  delivery_fee_cents INTEGER,
  currency VARCHAR(3) DEFAULT 'USD',
  -- Delivery zone — PostGIS polygon
  -- Only one branch per polygon. Overlaps trigger GOV-08 check.
  delivery_zone GEOGRAPHY(Polygon),
  tax_jurisdiction VARCHAR(5),
  is_active BOOLEAN DEFAULT true,
  created_at, updated_at
)

-- Branch availability (product-level enable/disable)
-- This feeds the availability cascade (Layer 2 and Layer 4)
branch_catalog_overrides (
  id, branch_id, product_id,
  enabled BOOLEAN,       -- Layer 2: branch carries this item at all?
  override_type VARCHAR, -- 'branch_setting' | 'date_override' | 'weekly_schedule'
  day_of_week INTEGER,   -- 0-6, null for date overrides
  override_date DATE,    -- null for weekly schedule
  reason TEXT,
  changed_by_user_id UUID,
  created_at
)

-- Audit log (append-only, partitioned by month)
audit_log (
  id, franchise_id, branch_id, actor_id, actor_role,
  action VARCHAR, resource_type VARCHAR, resource_id UUID,
  before_value JSONB, after_value JSONB,
  ip_address INET, device_id VARCHAR,
  created_at  -- partition key
)
```

## 2.3 API — Super admin

```
GET    /v1/admin/franchises                     → franchise directory (paginated)
POST   /v1/admin/franchises                     → create franchise (GOV-03)
GET    /v1/admin/franchises/{id}                → franchise detail
PATCH  /v1/admin/franchises/{id}                → update franchise
POST   /v1/admin/franchises/{id}/suspend        → suspend (GOV-06)
POST   /v1/admin/franchises/{id}/reactivate     → reactivate

GET    /v1/admin/branches                       → branch directory across network (SA-03)
POST   /v1/admin/branches                       → create branch (GOV-04)
GET    /v1/admin/branches/{id}                  → branch detail
PATCH  /v1/admin/branches/{id}                  → update branch
POST   /v1/admin/branches/{id}/suspend          → suspend branch (SA-05)
POST   /v1/admin/branches/{id}/reactivate

GET    /v1/admin/delivery-zones                 → all zones with overlap check
POST   /v1/admin/delivery-zones/check-overlap   → GOV-08 overlap detection
```

## 2.4 API — Franchise owner

```
GET    /v1/franchise/branches                   → own branches only
GET    /v1/franchise/branches/{id}              → own branch detail
PATCH  /v1/franchise/branches/{id}/hours        → opening hours
PATCH  /v1/franchise/branches/{id}/delivery     → delivery zone + fee
PATCH  /v1/franchise/branches/{id}/capacity     → slot config

GET    /v1/franchise/staff                      → staff at own branches
POST   /v1/franchise/staff                      → create staff account
PATCH  /v1/franchise/staff/{id}                 → update role/status
DELETE /v1/franchise/staff/{id}                 → deactivate
```

## 2.5 API — Branch manager

```
GET    /v1/branch/config                        → own branch config
PATCH  /v1/branch/config/hours                  → exception dates
PATCH  /v1/branch/config/capacity               → slot capacity, cut-off
POST   /v1/branch/emergency-close               → close all today's slots
GET    /v1/branch/catalog                       → availability grid
PATCH  /v1/branch/catalog/{productId}/available → enable/disable item (CAT-05)
PATCH  /v1/branch/catalog/bulk                  → bulk enable/disable (CAT-08)
```

## 2.6 Delivery zone overlap check (GOV-08)

When a new delivery zone polygon is submitted:

1. Load all active branch delivery zones in the same region (PostGIS query).
2. Check `ST_Overlaps(new_zone, existing_zone)` for each.
3. If any overlap: return `409 DELIVERY_ZONE_OVERLAP` with conflicting branch details.
4. Never silently create an overlapping zone.

At routing time, if a customer address matches two zones (edge case): apply tie-break
(nearest by road distance → most remaining capacity → branch code).

## 2.7 Governance enforcement (GOV-02)

Every write to a governed field passes through `internal/governance/check.go`.

```go
type GovernanceLevel int
const (
  BrandOwned     GovernanceLevel = 1  // only super admin may write
  FranchiseOwned GovernanceLevel = 2  // franchise owner may write
  BranchOwned    GovernanceLevel = 3  // branch manager may write
)
```

The field-level governance matrix (Section 5.3 of BRD) is a Go constant map, not a DB
table — it changes only with code deployments, not admin clicks.

## 2.8 Tests

Integration: franchise CRUD round-trip. Branch creation inherits master catalogue.
Zone overlap: non-overlapping zones accepted, overlapping zones rejected with correct
conflict detail. Suspension stops routing within test assertion.

## Previous / Next

← [Phase 1 — Identity](phase1-identity.md) · [Phase 3 — Catalog](phase3-catalog.md) →
