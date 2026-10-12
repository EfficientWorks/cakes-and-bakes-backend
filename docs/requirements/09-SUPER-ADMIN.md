# Module 09 — Super Admin & Network Reporting

**Phase:** R1  
**BRD refs:** SA-01 to SA-16, REP-01 to REP-18, Section 11  
**Surfaces:** Super admin console

---

## Overview

Super admin serves two jobs:
1. **Network management** — bring branches/franchises live, keep the brand consistent
2. **Consolidated visibility** — one dashboard to see which locations are performing

---

## Network Dashboard

#### `GET /api/v1/super-admin/dashboard`
```
Response 200:
{
  "network": {
    "totalRevenue":       1245000,    // cents, today
    "totalOrders":        142,
    "avgOrderValue":      8767,
    "openOrders":         38,
    "fulfilledToday":     104
  },

  "liveStrip": {
    "inProduction":       24,
    "atRisk":             3,          // slots < 30 min away, not in production
    "branchesSuspended":  1,
    "unroutedOrders":     0
  },

  "branchLeagueTable": [
    {
      "branchId":          "uuid",
      "branchName":        "Edison, NJ",
      "franchiseName":     "Company-Owned",
      "revenueCents":      485000,
      "orderCount":        58,
      "avgOrderValue":     8362,
      "onTimeFulfilmentPct": 97.2,
      "rank":              1
    },
    {
      "branchId":    "uuid",
      "branchName":  "Trappe, PA",
      "revenueCents": 312000,
      "orderCount":   34,
      "rank":         2
    }
  ],

  "revenueTrend": [
    { "date": "2026-08-17", "totalCents": 980000 },
    { "date": "2026-08-18", "totalCents": 1123000 },
    { "date": "2026-08-19", "totalCents": 1245000 }
  ],

  "alerts": [
    {
      "type":    "BRANCH_SUSPENDED",
      "message": "North Wales PA is suspended",
      "branchId": "uuid"
    }
  ]
}
```

---

## Comparable Sales Report

BRD Section 11.3: Comparable sales compares ONLY branches that were trading in BOTH the current period AND the comparison period. New/closed/suspended branches are excluded and reported separately.

#### `GET /api/v1/super-admin/reports/comparable-sales`
```
Query:
  period      TEXT   this_week|this_month|this_quarter|this_year|custom
  from        DATE   (if period=custom)
  to          DATE
  compareTo   TEXT   prior_period|same_period_last_year
  maturityMonths INT  default 13 (min months trading to be "comparable")

Response 200:
{
  "period":       { "from": "2026-08-01", "to": "2026-08-31" },
  "comparedTo":   { "from": "2026-07-01", "to": "2026-07-31" },
  "maturityMonths": 13,

  "comparable": {
    "branches": ["Edison NJ", "Trappe PA"],
    "branchCount": 2,
    "currentRevenue":    945000,
    "priorRevenue":      820000,
    "growthPct":         15.2,
    "currentOrders":     108,
    "priorOrders":       94,
    "orderGrowthPct":    14.9
  },

  "nonComparable": {
    "branches": ["North Wales PA"],
    "reason":   "Opened within last 13 months",
    "currentRevenue": 312000,
    "note":     "Excluded from comparable figure — opened April 2026"
  },

  "exclusionRuleApplied": "Branches trading < 13 months in either period are excluded",

  "sideBySide": [
    {
      "branchName":       "Edison, NJ",
      "currentRevenue":   485000,
      "priorRevenue":     425000,
      "growthPct":        14.1,
      "currentOrders":    58,
      "priorOrders":      51,
      "avgOrderValue":    8362,
      "priorAvgOrder":    8333,
      "isComparable":     true
    },
    {
      "branchName":       "Trappe, PA",
      "currentRevenue":   460000,
      "priorRevenue":     395000,
      "growthPct":        16.5,
      "isComparable":     true
    }
  ]
}
```

---

## Network-Wide Reports

#### `GET /api/v1/super-admin/reports/sales`
```
Query: from, to, branchId[] (multi-select), franchiseId[], groupBy=day|week|month

Response: aggregated sales data across all selected branches
```

#### `GET /api/v1/super-admin/reports/side-by-side`
```
Query: branchIds[] (select 2-5), from, to

Response 200:
{
  "branches": [
    {
      "id":           "uuid",
      "name":         "Edison, NJ",
      "revenueCents": 485000,
      "orders":       58,
      "avgOrder":     8362,
      "growth":       "+14.1%"
    },
    {
      "id":    "uuid",
      "name":  "Trappe, PA",
      ...
    }
  ]
}
```

#### `GET /api/v1/super-admin/reports/products`
```
Network-wide product performance — which items sell best, and where.
Query: from, to, branchId[]
```

#### `GET /api/v1/super-admin/reports/channel`
```
Breakdown by channel: web | pos | phone
```

#### `GET /api/v1/super-admin/reports/fulfilment`
```
Pickup vs delivery split per branch
```

#### `GET /api/v1/super-admin/reports/peak-analysis`
```
Slot utilisation by day of week and time band — for capacity planning before Mother's Day etc.
Query: branchId, from, to

Response:
{
  "heatmap": [
    { "dayOfWeek": "saturday", "slot": "10:00–11:00", "avgUtilisationPct": 94 },
    { "dayOfWeek": "saturday", "slot": "11:00–12:00", "avgUtilisationPct": 100 }
  ]
}
```

#### `GET /api/v1/super-admin/reports/refunds-and-cancellations`
```
Per-branch refund and cancellation rates — early warning of operational problems.
```

#### `GET /api/v1/super-admin/reports/export`
```
Query: reportType, format=csv|pdf, + report-specific filters
Response: file download
```

---

## Master Catalogue Management

#### `GET /api/v1/super-admin/products`
#### `POST /api/v1/super-admin/products`
#### `PATCH /api/v1/super-admin/products/:id`
#### `DELETE /api/v1/super-admin/products/:id`

### Publish Catalogue Change to Network

#### `POST /api/v1/super-admin/products/:id/publish`
```
Body:
{
  "scope": "all" | "selected",
  "franchiseIds": ["uuid"],   // if scope=selected
  "branchIds":    ["uuid"]    // if scope=selected
}

Side effects:
  - Updates branch_product_rules for all targeted branches
  - Availability reflects on storefront within 30s

Response 200: { "publishedTo": 3 }
```

---

## Content Management (Brand-controlled)

#### `GET /api/v1/super-admin/content`
#### `PATCH /api/v1/super-admin/content/:key`
```
Manages all dynamic storefront content:
  - Homepage hero text, marquee, featured collections
  - Policy pages (returns, FAQ, privacy)
  - Email template content
```

---

## Exception Queue (Unrouted Orders)

#### `GET /api/v1/super-admin/routing-exceptions`
```
Orders that failed automatic routing.

Response 200:
{
  "data": [
    {
      "tempId":       "...",
      "purchaserEmail": "...",
      "deliveryPostcode": "07030",
      "reason":        "No branch serves this zone",
      "placedAt":      "2026-08-24T10:00:00Z"
    }
  ]
}
```

#### `POST /api/v1/super-admin/routing-exceptions/:id/assign`
```
Body: { "branchId": "uuid", "note": "Manually assigned — nearest branch agreed to fulfil" }
```

---

## Audit Log

#### `GET /api/v1/super-admin/audit`
```
Query: franchiseId, branchId, actorId, action, from, to, page, limit

Response 200:
{
  "data": [
    {
      "id":           "uuid",
      "actor":        "kajal@bakedbouquet.com",
      "actorRole":    "branch_manager",
      "action":       "order.amend",
      "resourceType": "order",
      "resourceId":   "uuid",
      "branchName":   "Edison, NJ",
      "beforeValue":  { "scheduledDate": "2026-08-24" },
      "afterValue":   { "scheduledDate": "2026-08-25" },
      "reason":       "Customer requested change",
      "ipAddress":    "192.168.1.1",
      "createdAt":    "2026-08-22T09:15:00Z"
    }
  ]
}
```

---

## Staff Network Directory

#### `GET /api/v1/super-admin/staff`
```
All staff across all franchises and branches.
Query: role, franchiseId, branchId, isActive, search

Response 200:
{
  "data": [
    {
      "id":           "uuid",
      "name":         "Kajal Singh",
      "email":        "kajal@bb.com",
      "role":         "branch_manager",
      "franchise":    "Company-Owned",
      "branches":     ["Edison, NJ"],
      "lastActive":   "2026-08-22T09:00:00Z"
    }
  ]
}
```
