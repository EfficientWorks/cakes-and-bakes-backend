# Module 08 — Branch & Franchise Admin

**Phase:** R1  
**BRD refs:** ADM-01 to ADM-24, Section 12  
**Surfaces:** Branch admin portal, franchise admin portal

---

## Overview

The branch admin is the daily working tool. Its most important quality is **speed** — a manager on the phone to a customer must find and change an order in under 10 seconds.

Access is scoped:
- **Franchise owner** — sees all branches they own, consolidated and individually
- **Franchise viewer** — sales and performance only, no changes
- **Branch manager** — one branch, full operational control
- **Counter staff** — orders and payments within limits

---

## Staff Auth (Admin Portal)

Staff login is separate from customer accounts.

```sql
staff_users (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  email           TEXT UNIQUE NOT NULL,
  password_hash   TEXT NOT NULL,         -- Argon2id
  first_name      TEXT NOT NULL,
  last_name       TEXT NOT NULL,
  is_active       BOOLEAN DEFAULT true,
  created_at      TIMESTAMPTZ DEFAULT NOW()
)

staff_role_assignments (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id      UUID NOT NULL REFERENCES staff_users(id),
  role         TEXT NOT NULL,
               -- super_admin | finance | franchise_owner | franchise_viewer
               -- branch_manager | counter_staff | kitchen
  scope_type   TEXT NOT NULL,   -- brand | franchise | branch
  franchise_id UUID REFERENCES franchises(id),
  branch_id    UUID REFERENCES branches(id),
  limits       JSONB DEFAULT '{}',
               -- { "max_refund_cents": 5000, "max_discount_pct": 20 }
  created_at   TIMESTAMPTZ DEFAULT NOW()
)
```

#### `POST /api/v1/staff/auth/login`
```
Body: { "email": "...", "password": "..." }
Response 200: { "accessToken": "...", "refreshToken": "...", "user": { ... } }
```

---

## Order Management

### Order List

#### `GET /api/v1/admin/orders`
```
Query params:
  branchId        UUID   (required unless franchise_owner / super_admin)
  franchiseId     UUID   (for franchise owner — all their branches)
  date            DATE   (filter by scheduled_date)
  dateFrom        DATE
  dateTo          DATE
  status          TEXT   placed|confirmed|scheduled|in_production|ready|fulfilled|cancelled
  fulfilmentType  TEXT   pickup|delivery
  channel         TEXT   web|pos|phone
  search          TEXT   (order reference, purchaser name, recipient name, phone)
  page            INT    default 1
  limit           INT    default 25
  sort            TEXT   scheduled_date_asc|placed_at_desc

Response 200:
{
  "data": [
    {
      "id":               "uuid",
      "reference":        "EZYODR123",
      "branchName":       "Edison, NJ",         // ADM-02 — branch column visible
      "fulfilmentType":   "delivery",
      "scheduledDate":    "2026-08-24",
      "slotStart":        "14:00",
      "slotEnd":          "15:00",
      "recipientName":    "John Kelwin",         // RECIPIENT shown prominently
      "purchaserName":    "Jane Smith",
      "purchaserPhone":   "+17325559999",
      "status":           "placed",
      "totalCents":       36000,
      "itemCount":        3,
      "placedAt":         "2026-08-22T10:00:00Z",
      "isAtRisk":         false                  // true if slot time < 30 min away
    }
  ],
  "meta": { "page": 1, "total": 42 }
}
```

### Saved Views (ADM-03)

#### `GET /api/v1/admin/orders/views/today`
```
Shortcut for: date=today, status=placed|confirmed|in_production|ready
```

#### `GET /api/v1/admin/orders/views/tomorrow`
```
Shortcut for: date=tomorrow, status=placed|confirmed|scheduled
```

#### `GET /api/v1/admin/orders/views/unfulfilled`
```
Orders past their scheduled_date that are not fulfilled or cancelled
```

#### `GET /api/v1/admin/orders/views/at-risk`
```
Orders where scheduled slot is within 90 minutes and status is not in_production or later
```

### Order Detail

#### `GET /api/v1/admin/orders/:orderId`
```
Response 200:
{
  "id":        "uuid",
  "reference": "EZYODR123",
  "branchId":  "uuid",
  "branchName":"Edison, NJ",
  "franchiseId":"uuid",

  "recipient": {
    "firstName": "John",
    "lastName":  "Kelwin",
    "phone":     "+17325551234"
  },
  "giftMessage":  "Happy birthday!",
  "isGift":       true,

  "purchaser": {
    "firstName": "Jane",
    "lastName":  "Smith",
    "email":     "jane@example.com",
    "phone":     "+17325559999"
  },

  "fulfilmentType": "delivery",
  "deliveryAddress": {
    "street":   "412 Garden St",
    "city":     "Hoboken",
    "state":    "NJ",
    "postcode": "07030"
  },
  "deliveryInstructions": "Ring bell",
  "scheduledDate": "2026-05-23",
  "slotStart":     "11:00",
  "slotEnd":       "13:00",

  "items": [
    {
      "productName":     "Summer Sunset Cupcake Bouquet",
      "variantName":     "06 Cupcakes",
      "flavours":        ["Vanilla"],
      "addons":          [],
      "presentation":    "Ceramic Vase",
      "quantity":        5,
      "unitPriceCents":  5300,
      "lineTotalCents":  26500,
      "productionStatus": "pending"
    }
  ],

  "summary": {
    "subtotalCents":    24000,
    "deliveryFeeCents": 0,
    "taxCents":         2000,
    "tipCents":         0,
    "discountCents":    0,
    "totalCents":       36000
  },

  "status":        "placed",
  "paymentStatus": "captured",

  "statusTimeline": [
    { "status": "placed",    "at": "2026-05-20T14:00:00Z", "actor": "system" },
    { "status": "confirmed", "at": "2026-05-20T14:01:00Z", "actor": "system" }
  ],

  "amendments": [],
  "refunds":    [],
  "notes":      "",

  "canAmend":   true,
  "canCancel":  true,
  "canRefund":  true,
  "canFulfil":  true
}
```

### Create Manual Order (Telephone orders — ADM-10)

#### `POST /api/v1/admin/orders`
```
Body: same as storefront POST /orders but without payment (payment handled at counter or by link)
Response 201: order object with status="confirmed"
```

### Export Orders

#### `GET /api/v1/admin/orders/export`
```
Query: same filters as order list + format=csv|pdf
Response: file download
```

---

## Catalogue & Availability Control

See `04-CATALOG.md` for full catalogue endpoints.

### Quick Sold-Out Toggle (ADM-14)

#### `POST /api/v1/admin/branches/:branchId/products/:productId/sold-out`
Available directly from the order screen — one-tap without navigating away.

### Availability Grid (ADM-12)

#### `GET /api/v1/admin/branches/:branchId/availability`
Returns all products with their availability status per day — shown as a grid.

---

## Schedule & Capacity

### Get/Update Schedule Config

#### `GET /api/v1/admin/branches/:branchId/schedule`
```
Response 200:
{
  "slotDurationMins":   60,
  "pickupCutoffTime":   "16:00",
  "deliveryCutoffTime": "14:00",
  "maxOrdersPerSlot":   10,
  "defaultLeadHours":   2,
  "advanceDaysMax":     30
}
```

#### `PATCH /api/v1/admin/branches/:branchId/schedule`

### Slot Capacity

#### `GET /api/v1/admin/branches/:branchId/slots`
```
Query: date DATE, fulfilmentType TEXT

Response 200:
{
  "data": [
    { "slotStart": "09:00", "slotEnd": "10:00", "maxCapacity": 10, "reservedCount": 3, "remaining": 7 },
    { "slotStart": "10:00", "slotEnd": "11:00", "maxCapacity": 10, "reservedCount": 10, "remaining": 0 }
  ]
}
```

#### `PATCH /api/v1/admin/branches/:branchId/slots/:slotId`
```
Body: { "maxCapacity": 15 }   // temporary uplift for peak day
```

---

## Staff Management

#### `GET /api/v1/admin/staff`
```
Query: branchId, franchiseId, role

Response 200:
{
  "data": [
    {
      "id":         "uuid",
      "firstName":  "Kajal",
      "lastName":   "Singh",
      "email":      "kajal@bakedbouquet.com",
      "role":       "branch_manager",
      "branches":   ["Edison, NJ"],
      "isActive":   true,
      "lastActive": "2026-08-22T09:00:00Z",
      "limits": {
        "maxRefundCents": 10000,
        "maxDiscountPct": 20
      }
    }
  ]
}
```

#### `POST /api/v1/admin/staff/invite`
```
Body:
{
  "email":      "staff@example.com",
  "firstName":  "...",
  "lastName":   "...",
  "role":       "counter_staff",
  "branchIds":  ["uuid"],
  "limits": {
    "maxRefundCents": 5000
  }
}
Sends invitation email with set-password link.
Response 201: staff user object
```

#### `PATCH /api/v1/admin/staff/:staffId`
#### `POST /api/v1/admin/staff/:staffId/deactivate`

---

## Branch Dashboard

#### `GET /api/v1/admin/branches/:branchId/dashboard`
```
Response 200:
{
  "today": {
    "revenue":         125000,    // cents
    "orderCount":      14,
    "openOrders":      8,
    "atRiskOrders":    1,
    "fulfilledOrders": 5
  },
  "slotUtilisation": [
    { "slot": "09:00–10:00", "booked": 8, "capacity": 10, "pct": 80 },
    { "slot": "10:00–11:00", "booked": 10, "capacity": 10, "pct": 100, "isFull": true }
  ],
  "upcomingPreorders": [
    { "date": "2026-08-25", "orderCount": 6, "revenue": 53400 }
  ],
  "recentOrders": [
    { "reference": "EZYODR123", "recipient": "John K.", "status": "in_production", "time": "14:00–15:00" }
  ]
}
```

---

## Customer Management (Branch-scoped)

#### `GET /api/v1/admin/customers`
```
Query: branchId (required), search (name|email|phone), page, limit

Response 200:
{
  "data": [
    {
      "id":         "uuid",
      "firstName":  "Jane",
      "lastName":   "Smith",
      "email":      "jane@example.com",
      "phone":      "+17325559999",
      "orderCount": 3,
      "totalSpent": 24300,
      "lastOrderAt": "2026-08-01T12:30:00Z",
      "tags":       ["VIP"],
      "notes":      "Prefers no nuts"
    }
  ]
}
```

#### `GET /api/v1/admin/customers/:customerId`
#### `PATCH /api/v1/admin/customers/:customerId`
```
Body: { "tags": ["VIP", "allergy-nuts"], "notes": "..." }
```

---

## Local Reports (Branch Admin)

#### `GET /api/v1/admin/reports/sales`
```
Query: branchId, from, to, groupBy=day|week|month

Response 200:
{
  "data": [
    { "date": "2026-08-01", "revenue": 145000, "orderCount": 18, "avgOrderValue": 8055 }
  ],
  "summary": {
    "totalRevenue":    290000,
    "totalOrders":     36,
    "avgOrderValue":   8055,
    "vsLastPeriod":    "+12%"
  }
}
```

#### `GET /api/v1/admin/reports/products`
```
Query: branchId, from, to

Response 200:
{
  "data": [
    { "productName": "Berry Blossom Cupcake Bouquet", "unitsSold": 42, "revenue": 369600 },
    ...
  ]
}
```

#### `GET /api/v1/admin/reports/refunds`
#### `GET /api/v1/admin/reports/amendments`
#### `GET /api/v1/admin/reports/fulfilment-performance`
