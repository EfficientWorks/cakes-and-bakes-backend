# Module 02 — Tenancy, Franchise & Branch Governance

**Phase:** R1  
**BRD refs:** GOV-01 to GOV-12, Section 5  
**Surfaces:** Super admin console, branch admin

---

## Overview

Three-level hierarchy: **Brand → Franchise → Branch**. Every order, payment, staff record, and device belongs to exactly one franchise and one branch. A company-owned location is modelled as a franchise whose owner is the brand — one code path for all.

```
Brand: Baked Bouquet (id: fixed constant)
  ├── Franchise: Company-Owned NJ (franchise_id: uuid)
  │     ├── Branch: Edison NJ       (branch_id: uuid)
  │     └── Branch: North Wales PA  (branch_id: uuid)
  └── Franchise: Florida Franchisee (franchise_id: uuid)
        ├── Branch: Miami FL        (branch_id: uuid)
        └── Branch: Tampa FL        (branch_id: uuid)
```

---

## Database Tables

```sql
-- Franchises
franchises (
  id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name              TEXT NOT NULL,
  legal_entity      TEXT NOT NULL,
  primary_contact   JSONB NOT NULL,       -- {name, email, phone}
  contract_ref      TEXT,
  commercial_terms  JSONB,                -- {royalty_rate, fee_basis, payment_terms}
  territory         TEXT,
  status            TEXT NOT NULL DEFAULT 'pending',
                    -- pending | active | suspended | closed
  is_company_owned  BOOLEAN DEFAULT false,
  onboarded_at      TIMESTAMPTZ,
  created_at        TIMESTAMPTZ DEFAULT NOW(),
  updated_at        TIMESTAMPTZ DEFAULT NOW()
)

-- Branches
branches (
  id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  franchise_id        UUID NOT NULL REFERENCES franchises(id),
  name                TEXT NOT NULL,
  code                TEXT UNIQUE NOT NULL,     -- e.g. "EDISON-NJ"
  type                TEXT DEFAULT 'retail',    -- retail | central_kitchen
  address             JSONB NOT NULL,           -- {street, city, state, postcode, country}
  geolocation         GEOGRAPHY(POINT) NOT NULL,
  timezone            TEXT NOT NULL,            -- named zone e.g. "America/New_York"
  opening_hours       JSONB NOT NULL,           -- [{day, open, close}, ...] null = closed
  exception_dates     JSONB DEFAULT '[]',       -- [{date, reason, open, close}]
  fulfilment_types    TEXT[] DEFAULT ARRAY['pickup','delivery'],
  delivery_zone       JSONB,                    -- GeoJSON polygon or zip list
  delivery_fee_cents  INTEGER DEFAULT 0,
  min_order_cents     INTEGER DEFAULT 0,
  tax_jurisdiction    JSONB,                    -- {state, county, rate_pct}
  status              TEXT NOT NULL DEFAULT 'pending',
                      -- pending | active | suspended | closed
  created_at          TIMESTAMPTZ DEFAULT NOW(),
  updated_at          TIMESTAMPTZ DEFAULT NOW()
)

-- Governance field-level control
governance_rules (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  field_key   TEXT UNIQUE NOT NULL,   -- e.g. "product.base_price", "branch.delivery_fee"
  level       TEXT NOT NULL,          -- brand | franchise | branch
  description TEXT,
  updated_at  TIMESTAMPTZ DEFAULT NOW()
)
```

---

## Franchise API Endpoints

All endpoints require `Authorization: Bearer <staff_token>` with super_admin role unless noted.

### List Franchises

#### `GET /api/v1/admin/franchises`
```
Query: page, limit, status, search

Response 200:
{
  "data": [
    {
      "id":           "uuid",
      "name":         "Company-Owned NJ",
      "legalEntity":  "Baked Bouquet LLC",
      "branchCount":  2,
      "status":       "active",
      "onboardedAt":  "2026-01-15T00:00:00Z",
      "contractRef":  "BB-FRAN-001"
    }
  ],
  "meta": { "page": 1, "total": 4 }
}
```

### Create Franchise

#### `POST /api/v1/admin/franchises`
```
Body:
{
  "name":           "Florida Franchisee LLC",
  "legalEntity":    "Sunshine Bouquets Inc",
  "primaryContact": { "name": "...", "email": "...", "phone": "..." },
  "contractRef":    "BB-FRAN-002",
  "territory":      "Florida",
  "commercialTerms": {
    "royaltyRate": 0.06,
    "feeBasis":    "revenue",
    "paymentTerms": "monthly"
  }
}

Response 201: franchise object
Side effects:
  - Creates initial franchise_owner user and sends invitation email
  - Audit event logged
```

### Get / Update Franchise

#### `GET /api/v1/admin/franchises/:franchiseId`
#### `PATCH /api/v1/admin/franchises/:franchiseId`
```
Patchable: name, legalEntity, primaryContact, contractRef, territory, commercialTerms
Brand-locked fields rejected with 403 GOVERNANCE_VIOLATION
```

### Suspend / Reactivate Franchise

#### `POST /api/v1/admin/franchises/:franchiseId/suspend`
```
Body: { "reason": "Contract breach — payment overdue" }
Side effects:
  - All branches under franchise set to suspended
  - Storefront routing stops including these branches within 30s
  - Staff access revoked
  - In-flight orders (PLACED/CONFIRMED) flagged for review — NOT auto-cancelled
Response 200: { "success": true, "data": { "affectedBranches": 2, "openOrders": 5 } }
```

#### `POST /api/v1/admin/franchises/:franchiseId/reactivate`

---

## Branch API Endpoints

### List Branches

#### `GET /api/v1/admin/branches`
```
Query: franchiseId, status, page, limit

Response 200:
{
  "data": [
    {
      "id":          "uuid",
      "franchiseId": "uuid",
      "name":        "Edison, NJ",
      "code":        "EDISON-NJ",
      "status":      "active",
      "type":        "retail",
      "todayOrders": 14,
      "todayRevenue": 182500   // cents
    }
  ]
}
```

### Create Branch

#### `POST /api/v1/admin/branches`
```
Body:
{
  "franchiseId":   "uuid",
  "name":          "Edison, NJ",
  "code":          "EDISON-NJ",
  "type":          "retail",
  "address": {
    "street":   "180 Lafayette Ave",
    "city":     "Edison",
    "state":    "NJ",
    "postcode": "08837",
    "country":  "US"
  },
  "timezone":      "America/New_York",
  "openingHours": [
    { "day": "monday",    "open": "09:00", "close": "18:00" },
    { "day": "tuesday",   "open": "09:00", "close": "18:00" },
    { "day": "wednesday", "open": "09:00", "close": "18:00" },
    { "day": "thursday",  "open": "09:00", "close": "18:00" },
    { "day": "friday",    "open": "09:00", "close": "18:00" },
    { "day": "saturday",  "open": "10:00", "close": "16:00" },
    { "day": "sunday",    "open": null,    "close": null }
  ],
  "fulfilmentTypes": ["pickup", "delivery"],
  "deliveryZone": {
    "type":  "zip_list",
    "zips":  ["08837", "08820", "08830"]
  },
  "deliveryFeeCents":  2500,
  "minOrderCents":     5000,
  "taxJurisdiction": {
    "state":   "NJ",
    "county":  "Middlesex",
    "ratePct": 0.06625
  }
}

Response 201: branch object
Side effects:
  - Branch inherits master catalogue automatically
  - branch_product_rules created for all active products (available=true)
  - Audit event logged
  - Branch can accept orders same day (NFR-14)
```

### Get / Update Branch

#### `GET /api/v1/admin/branches/:branchId`
#### `PATCH /api/v1/admin/branches/:branchId`
```
Patchable by level:
  brand level:     name, code (not post-go-live)
  franchise level: deliveryZone, deliveryFeeCents, minOrderCents, fulfilmentTypes
  branch level:    openingHours, exceptionDates, taxJurisdiction

Governance enforced server-side.
```

### Branch Opening Hours & Exceptions

#### `PUT /api/v1/admin/branches/:branchId/hours`
```
Body: { "hours": [ { "day": "monday", "open": "09:00", "close": "18:00" } ... ] }
```

#### `POST /api/v1/admin/branches/:branchId/exceptions`
```
Body:
{
  "date":   "2026-11-27",
  "reason": "Thanksgiving",
  "open":   null,
  "close":  null      // null = closed all day
}
```

#### `DELETE /api/v1/admin/branches/:branchId/exceptions/:exceptionId`

### Emergency Close

#### `POST /api/v1/admin/branches/:branchId/emergency-close`
```
Body: { "reason": "Equipment failure — oven broke" }

Pre-response preview (shown to admin before confirming):
{
  "affectedSlots":  8,
  "affectedOrders": 14,
  "message": "Closing will affect 14 orders scheduled for today. Continue?"
}

On confirm=true:
  - All remaining today's slots set to capacity=0
  - Affected customers notified by email
  - Branch status set to "emergency_closed" for today only
  - Resets to active at midnight branch timezone

Response 200: { "success": true, "data": { "affectedOrders": 14 } }
```

### Branch Suspension / Reactivation

#### `POST /api/v1/admin/branches/:branchId/suspend`
#### `POST /api/v1/admin/branches/:branchId/reactivate`

---

## Delivery Zone Validation

#### `POST /api/v1/storefront/check-delivery`
```
Body: { "postcode": "08837" }
OR
Body: { "lat": 40.5187, "lng": -74.4121 }

Response 200:
{
  "eligible": true,
  "branch": {
    "id":              "uuid",
    "name":            "Edison, NJ",
    "deliveryFeeCents": 2500,
    "minOrderCents":    5000,
    "nextSlot":        "2026-08-24T14:00:00Z"
  }
}

Response 200 (not eligible):
{
  "eligible": false,
  "nearestBranches": [
    {
      "name":      "Trappe, PA",
      "distance":  "8.9 miles",
      "offersPickup": true
    }
  ],
  "emailCapturePrompt": true
}
```

#### `POST /api/v1/storefront/zone-coverage-notify`
```
Body: { "email": "customer@example.com", "postcode": "07030" }
Stores for future coverage notification
Response 200: { "success": true }
```

---

## Governance Rules API (Super Admin only)

#### `GET /api/v1/admin/governance`
```
Response 200:
{
  "data": [
    { "fieldKey": "product.base_price",       "level": "brand",    "description": "Base price set by brand only" },
    { "fieldKey": "branch.delivery_fee_cents", "level": "franchise","description": "Franchise sets delivery fee" },
    { "fieldKey": "branch.opening_hours",      "level": "branch",   "description": "Branch sets own hours" }
  ]
}
```

#### `PATCH /api/v1/admin/governance/:fieldKey` — Phase 2

---

## Branch Resolution for Storefront Orders

Used internally at order creation — not a public endpoint.

```typescript
// Routing algorithm (server-side)
async function resolveBranch(input: {
  fulfilmentType: 'pickup' | 'delivery',
  pickupBranchId?: string,
  deliveryAddress?: Address,
  requestedSlot: DateTime,
}): Promise<Branch> {

  if (fulfilmentType === 'pickup') {
    return getBranchById(pickupBranchId);
  }

  // Delivery: match zones
  const candidates = await getBranchesForZone(deliveryAddress.postcode);

  if (candidates.length === 0) {
    throw new RoutingException('NO_BRANCH_SERVES_ZONE');
  }

  if (candidates.length === 1) {
    return candidates[0];
  }

  // Tie-break: nearest by road distance → remaining slot capacity → branch code
  return candidates
    .sort((a, b) =>
      a.distanceMiles - b.distanceMiles ||
      b.remainingSlotCapacity - a.remainingSlotCapacity ||
      a.code.localeCompare(b.code)
    )[0];
}
```

---

## Audit Events

Every governance action writes to `audit_log`:

```sql
audit_log (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  actor_id      UUID NOT NULL,
  actor_role    TEXT NOT NULL,
  action        TEXT NOT NULL,      -- e.g. "branch.suspend", "franchise.create"
  resource_type TEXT NOT NULL,
  resource_id   UUID NOT NULL,
  franchise_id  UUID,
  branch_id     UUID,
  before_value  JSONB,
  after_value   JSONB,
  reason        TEXT,
  ip_address    INET,
  device_id     UUID,
  created_at    TIMESTAMPTZ DEFAULT NOW()
  -- APPEND ONLY — no UPDATE or DELETE ever
)
```
