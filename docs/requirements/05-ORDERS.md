# Module 05 — Orders, Scheduling, Cart & Checkout

**Phase:** R1  
**BRD refs:** SF-17 to SF-29, SF-30 to SF-33, ORD-01 to ORD-17, SCH-01 to SCH-09  
**Surfaces:** Customer storefront, branch admin

---

## Overview

The ordering flow has four stages:
1. **Branch resolution** — ZIP check, branch selected
2. **Product configuration** — progressive steps (size → flavour → add-ons → gift → fulfillment)
3. **Scheduling** — slot selection against real branch capacity
4. **Checkout** — recipient first, purchaser second, payment third

---

## Database Tables

```sql
-- Slot capacity per branch
slot_capacity (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  branch_id       UUID NOT NULL REFERENCES branches(id),
  date            DATE NOT NULL,
  slot_start      TIME NOT NULL,
  slot_end        TIME NOT NULL,
  fulfilment_type TEXT NOT NULL,       -- pickup | delivery
  max_capacity    INTEGER NOT NULL,
  reserved_count  INTEGER DEFAULT 0,   -- incremented atomically at order creation
  UNIQUE(branch_id, date, slot_start, fulfilment_type)
)

-- Scheduling configuration per branch
branch_schedule_config (
  id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  branch_id             UUID UNIQUE NOT NULL REFERENCES branches(id),
  slot_duration_mins    INTEGER DEFAULT 60,
  pickup_cutoff_time    TIME NOT NULL,       -- e.g. 16:00 for same-day pickup
  delivery_cutoff_time  TIME NOT NULL,       -- e.g. 14:00 for same-day delivery
  max_orders_per_slot   INTEGER DEFAULT 10,
  default_lead_hours    INTEGER DEFAULT 2,
  advance_days_max      INTEGER DEFAULT 30,  -- how far ahead customer can order
  updated_at            TIMESTAMPTZ DEFAULT NOW()
)

-- Orders
orders (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  reference       TEXT UNIQUE NOT NULL,      -- e.g. "EZYODR123"
  franchise_id    UUID NOT NULL REFERENCES franchises(id),
  branch_id       UUID NOT NULL REFERENCES branches(id),
  customer_id     UUID REFERENCES customers(id),  -- null for guest
  channel         TEXT NOT NULL,             -- web | pos | phone | kds_manual
  fulfilment_type TEXT NOT NULL,             -- pickup | delivery

  -- Recipient (BRD: shown FIRST and PROMINENTLY)
  recipient_first_name TEXT NOT NULL,
  recipient_last_name  TEXT NOT NULL,
  recipient_phone      TEXT,                 -- required for delivery
  gift_message         TEXT,                 -- max 120 chars
  is_gift              BOOLEAN DEFAULT true,

  -- Purchaser
  purchaser_first_name TEXT NOT NULL,
  purchaser_last_name  TEXT NOT NULL,
  purchaser_email      TEXT NOT NULL,
  purchaser_phone      TEXT,

  -- Delivery address (null for pickup)
  delivery_street  TEXT,
  delivery_city    TEXT,
  delivery_state   TEXT,
  delivery_postcode TEXT,
  delivery_instructions TEXT,

  -- Pickup branch (same as branch_id for pickup)
  pickup_branch_id UUID REFERENCES branches(id),

  -- Scheduling
  scheduled_date   DATE NOT NULL,
  slot_start       TIME NOT NULL,
  slot_end         TIME NOT NULL,
  slot_capacity_id UUID REFERENCES slot_capacity(id),

  -- Financials (all in cents)
  subtotal_cents       INTEGER NOT NULL,
  delivery_fee_cents   INTEGER DEFAULT 0,
  tax_cents            INTEGER NOT NULL,
  tip_cents            INTEGER DEFAULT 0,
  discount_cents       INTEGER DEFAULT 0,
  total_cents          INTEGER NOT NULL,
  currency             TEXT DEFAULT 'USD',

  -- Status
  status  TEXT NOT NULL DEFAULT 'placed',
  -- placed | confirmed | scheduled | in_production | ready | out_for_delivery | fulfilled | cancelled

  -- Payment
  stripe_payment_intent_id TEXT,
  payment_status           TEXT DEFAULT 'pending',  -- pending | captured | refunded | partial_refund

  -- Meta
  idempotency_key  TEXT UNIQUE NOT NULL,
  marketing_opt_in BOOLEAN DEFAULT false,
  notes            TEXT,                    -- internal staff notes
  created_at       TIMESTAMPTZ DEFAULT NOW(),
  updated_at       TIMESTAMPTZ DEFAULT NOW()
)

-- Order line items
order_items (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id        UUID NOT NULL REFERENCES orders(id),
  product_id      UUID NOT NULL REFERENCES products(id),
  product_name    TEXT NOT NULL,           -- denormalised for history integrity
  variant_id      UUID REFERENCES product_variants(id),
  variant_name    TEXT,
  quantity        INTEGER NOT NULL DEFAULT 1,
  unit_price_cents INTEGER NOT NULL,
  line_total_cents INTEGER NOT NULL,

  -- Configuration snapshot
  selected_flavours   JSONB,     -- [{id, name, priceDelta}]
  selected_addons     JSONB,     -- [{id, name, priceDelta}]
  selected_presentation JSONB,   -- {id, name, priceDelta}
  selected_wrap_colour  JSONB,   -- {id, name, colourHex}

  -- Kitchen production status
  production_status  TEXT DEFAULT 'pending',  -- pending | in_progress | done
  production_started_at TIMESTAMPTZ,
  production_done_at    TIMESTAMPTZ,
  production_by_user_id UUID
)

-- Order amendments log
order_amendments (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id     UUID NOT NULL REFERENCES orders(id),
  actor_id     UUID NOT NULL,
  actor_role   TEXT NOT NULL,
  reason       TEXT NOT NULL,
  before_value JSONB NOT NULL,
  after_value  JSONB NOT NULL,
  price_delta_cents INTEGER DEFAULT 0,   -- + for increase, - for decrease
  created_at   TIMESTAMPTZ DEFAULT NOW()
)

-- Order status timeline
order_status_events (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id   UUID NOT NULL REFERENCES orders(id),
  status     TEXT NOT NULL,
  actor_id   UUID,
  actor_type TEXT,           -- customer | staff | system
  note       TEXT,
  created_at TIMESTAMPTZ DEFAULT NOW()
)

-- Discount codes
discount_codes (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  franchise_id    UUID REFERENCES franchises(id),  -- null = brand-wide
  code            TEXT UNIQUE NOT NULL,
  type            TEXT NOT NULL,            -- fixed | percentage | free_delivery
  value           INTEGER NOT NULL,         -- cents for fixed, basis points for pct
  min_order_cents INTEGER DEFAULT 0,
  max_uses        INTEGER,                  -- null = unlimited
  times_used      INTEGER DEFAULT 0,
  valid_from      TIMESTAMPTZ,
  valid_until     TIMESTAMPTZ,
  is_active       BOOLEAN DEFAULT true,
  created_at      TIMESTAMPTZ DEFAULT NOW()
)
```

---

## Scheduling Endpoints

### Get Available Slots

#### `GET /api/v1/storefront/slots`
```
Query params:
  branchId        UUID   (required)
  fulfilmentType  TEXT   (pickup | delivery)
  startDate       DATE   (optional, defaults to today)
  days            INT    (optional, defaults to 14 — how many days to return)
  productIds      UUID[] (optional — filters out dates where items unavailable)

Algorithm:
  1. For each date in range:
     a. Check branch open hours for that day (exception dates override)
     b. Check all requested products are available on that date (4-layer cascade)
     c. Check cut-off time hasn't passed for today
     d. Return slots with remaining capacity

Response 200:
{
  "data": [
    {
      "date":  "2026-08-24",
      "label": "Today",
      "slots": [
        { "id": "slot_uuid", "start": "14:00", "end": "15:00", "remaining": 7 },
        { "id": "slot_uuid", "start": "15:00", "end": "16:00", "remaining": 3 }
      ]
    },
    {
      "date":  "2026-08-25",
      "label": "Tomorrow",
      "slots": [
        { "id": "slot_uuid", "start": "09:00", "end": "10:00", "remaining": 10 },
        ...
      ]
    }
  ],
  "cutoffNote": "Order within 2h 14m for delivery today"
}

Slots with remaining=0 are NOT returned (customer never sees full slots).
```

---

## Cart Endpoints

Cart is session-based (no DB persistence for guests; stored in Redis for 2 hours).
For logged-in customers, cart is linked to customer_id.

### Get Cart

#### `GET /api/v1/storefront/cart`
```
Headers: Authorization (optional), X-Cart-Token (for guests)

Response 200:
{
  "cartId":  "...",
  "branchId": "...",
  "branchName": "Edison, NJ",
  "fulfilmentType": "delivery",
  "deliveryAddress": { "street": "80 Lafayette Ave Edison", "city": "Edison", "state": "NJ", "postcode": "08837" },
  "scheduledDate": "2026-04-24",
  "slotStart": "14:00",
  "slotEnd":   "15:00",

  "items": [
    {
      "cartItemId": "...",
      "productId":  "...",
      "productName":"Summer Sunset Cupcake Bouquet",
      "productImage": "...",
      "variantName": "06 Cupcakes",
      "flavours":   ["Vanilla"],
      "addons":     [],
      "presentation": "Ceramic Vase",
      "quantity":   5,
      "unitPrice":  5300,
      "lineTotal":  26400,
      "recipient": {
        "firstName": "John",
        "lastName":  "Kelwin",
        "phone":     null
      }
    }
  ],

  "upsellAddons": [
    { "id": "...", "name": "It's a Girl", "price": 300, "imageUrl": "..." }
  ],

  "summary": {
    "subtotal":    34600,
    "deliveryFee": 0,
    "taxJurisdiction": "Edison, NJ",
    "estimatedTax": 2292,
    "tip":         0,
    "total":       36888,
    "currency":    "USD"
  }
}
```

### Add to Cart

#### `POST /api/v1/storefront/cart/items`
```
Body:
{
  "productId":    "uuid",
  "variantId":    "uuid",
  "quantity":     1,
  "selectedFlavourIds":     ["uuid-vanilla"],
  "selectedAddonIds":       ["uuid-birthday"],
  "selectedPresentationId": "uuid-ceramic-vase",
  "selectedWrapColourId":   "uuid-pink",
  "isGift":       true,
  "recipient": {
    "firstName": "Mary",
    "lastName":  "Jones"
  },
  "giftMessage": "Happy birthday — eat the flowers."
}

Side effects:
  - Cart auto-created if not exists, scoped to branch
  - If different branch than existing cart: return 409 CART_BRANCH_CONFLICT
  - Returns cartId and X-Cart-Token for guests

Response 201:
{
  "cartItemId": "...",
  "cartId":     "...",
  "cartToken":  "...",    // for guests: store in cookie
  "cart":       { ... }   // full cart object
}

Errors:
  409 CART_BRANCH_CONFLICT — "Your cart contains items from [Edison NJ]. Change branch?"
  422 PRODUCT_UNAVAILABLE
  422 VARIANT_REQUIRED
  422 FLAVOUR_REQUIRED
```

### Update / Remove Cart Item

#### `PATCH /api/v1/storefront/cart/items/:cartItemId`
```
Body: any of { quantity, selectedFlavourIds, selectedAddonIds, selectedPresentationId, recipient, giftMessage }
Response 200: full cart object
```

#### `DELETE /api/v1/storefront/cart/items/:cartItemId`

### Apply Discount Code

#### `POST /api/v1/storefront/cart/discount`
```
Body: { "code": "SUMMER20" }

Response 200:
{ "discount": { "code": "SUMMER20", "type": "percentage", "value": 20, "savingCents": 692 } }

Errors:
  404 CODE_NOT_FOUND
  400 CODE_EXPIRED
  400 MIN_ORDER_NOT_MET — "Minimum order $50 required for this code"
  400 CODE_MAX_USES_REACHED
```

#### `DELETE /api/v1/storefront/cart/discount`

### Set Tip

#### `POST /api/v1/storefront/cart/tip`
```
Body: { "percentage": 15 }  OR  { "fixedCents": 500 }
Response 200: updated cart summary
```

---

## Checkout Endpoints

### Validate Cart Before Checkout

#### `POST /api/v1/storefront/checkout/validate`
```
Runs pre-checkout validation:
  - All items still available at branch on scheduled date
  - Slot still has capacity
  - Prices unchanged (if changed, return warning)
  - Delivery address still in zone

Response 200:
{
  "valid": true,
  "warnings": []
}

Response 200 (with warnings):
{
  "valid": false,
  "warnings": [
    {
      "type": "PRICE_CHANGED",
      "message": "Berry Blossom price changed from $88.00 to $92.00",
      "productId": "..."
    },
    {
      "type": "SLOT_FULL",
      "message": "Your selected slot is now full. Please choose another time.",
      "suggestedSlots": [ ... ]
    }
  ]
}
```

### Create Order (Place Order)

#### `POST /api/v1/storefront/orders`
```
Headers:
  Authorization: Bearer <token>  (optional — guest allowed)
  Idempotency-Key: <uuid>        (required — prevents double submit)

Body:
{
  "cartId":     "...",
  "cartToken":  "...",     // for guests

  "recipient": {
    "firstName": "John",
    "lastName":  "Kelwin",
    "phone":     "+17325551234"   // required for delivery
  },
  "giftMessage":       "Happy birthday!",
  "isGift":            true,

  "purchaser": {
    "firstName": "Jane",
    "lastName":  "Smith",
    "email":     "jane@example.com",
    "phone":     "+17325559999"
  },

  "deliveryInstructions": "Leave at door, ring bell",
  "marketingOptIn":       false,

  "payment": {
    "method":             "card",
    "stripePaymentMethodId": "pm_xxx",
    "saveCard":           false
  }
}

Server-side at order creation:
  1. Validate all cart items available
  2. Atomically decrement slot_capacity.reserved_count
     (SELECT FOR UPDATE on slot_capacity row — prevents race condition)
  3. Calculate final price (server-side, never trust client)
  4. Capture Stripe payment
  5. Create order + order_items records
  6. Write order_status_events: 'placed'
  7. Publish real-time event to branch KDS channel (Phase 2)
  8. Queue confirmation email via BullMQ
  9. Return order confirmation

Response 201:
{
  "success": true,
  "data": {
    "orderId":    "uuid",
    "reference":  "EZYODR123",
    "status":     "placed",
    "trackingUrl": "https://bakedbouquet.com/track/EZYODR123",

    "recipient": {
      "firstName": "John",
      "lastName":  "Kelwin"
    },

    "items": [
      {
        "name":      "Summer Sunset Cupcake Bouquet",
        "variant":   "06 Cupcakes",
        "quantity":  5,
        "total":     26500
      }
    ],

    "fulfilmentType":   "delivery",
    "scheduledDate":    "2026-05-23",
    "slotStart":        "11:00",
    "slotEnd":          "13:00",
    "deliveryAddress":  "412 Garden St, Hoboken NJ 07030",
    "branchName":       "Edison, NJ",

    "summary": {
      "subtotal":   24000,
      "delivery":   0,
      "tax":        2000,
      "total":      36000,
      "currency":   "USD"
    },

    "estimatedEmail": "Receipt sent to jane@example.com"
  }
}

Errors:
  409 SLOT_NO_LONGER_AVAILABLE
  409 PRODUCT_UNAVAILABLE
  402 PAYMENT_FAILED — { "stripeError": "card_declined" }
  422 MISSING_RECIPIENT_PHONE — for delivery orders
  422 DELIVERY_ADDRESS_OUT_OF_ZONE
```

---

## Order Tracking (Public)

#### `GET /api/v1/orders/track/:reference`
```
No auth required — public endpoint, returns limited data only.
Reference must match order — prevents enumeration via UUID check.

Response 200:
{
  "reference":   "EZYODR123",
  "status":      "in_production",
  "statusLabel": "Your bouquet is being made",
  "recipient":   "John K.",             // truncated
  "fulfilmentType": "delivery",
  "scheduledDate":  "2026-05-23",
  "slotStart":   "11:00",
  "slotEnd":     "13:00",
  "timeline": [
    { "status": "placed",       "at": "2026-05-20T14:00:00Z", "label": "Order placed" },
    { "status": "confirmed",    "at": "2026-05-20T14:01:00Z", "label": "Order confirmed" },
    { "status": "in_production","at": "2026-05-23T09:00:00Z", "label": "Being prepared" }
  ]
}
```

---

## Order Amendment (Staff / Customer)

#### `PATCH /api/v1/admin/orders/:orderId`
```
Auth: staff token with order amendment permission

Pre-condition: status must be placed | confirmed | scheduled (not in_production or later)

Body (any amendable fields):
{
  "recipientFirstName":   "Mary",
  "recipientLastName":    "Jones",
  "recipientPhone":       "+17325559999",
  "giftMessage":          "Updated message",
  "scheduledDate":        "2026-08-25",
  "slotStart":            "10:00",
  "deliveryInstructions": "Call on arrival",
  "reason":               "Customer called to update"
}

Server-side:
  1. If slot changed: release old slot capacity, reserve new slot atomically
  2. If price-affecting fields changed: compute delta, trigger payment adjustment
  3. Write order_amendments record with full before/after
  4. If order on KDS: publish amendment event to branch channel
  5. Queue customer amendment confirmation email
  6. Write audit_log

Response 200: full updated order object
Errors:
  409 ORDER_IN_PRODUCTION — cannot amend
  409 SLOT_UNAVAILABLE
```

---

## Order Status Management (Staff)

#### `POST /api/v1/admin/orders/:orderId/status`
```
Body: { "status": "fulfilled", "note": "Delivered to door" }

Valid transitions:
  placed → confirmed (auto or manual)
  confirmed → scheduled (future-dated)
  confirmed/scheduled → in_production (kitchen starts)
  in_production → ready
  ready → out_for_delivery | fulfilled (pickup)
  out_for_delivery → fulfilled
  any pre-production → cancelled

Response 200: updated order
```

#### `POST /api/v1/admin/orders/:orderId/mark-fulfilled`
```
R1 manual flow (before KDS is live):
Body: { "note": "Order handed to customer" }
Response 200: { "success": true, "status": "fulfilled" }
```

---

## Cancellation

#### `POST /api/v1/admin/orders/:orderId/cancel`
#### `POST /api/v1/account/orders/:orderId/cancel` (customer self-service)
```
Body: { "reason": "Customer requested" }

Cancellation tiers (BRD ORD-16):
  - Before cut-off:       full refund
  - Middle window:        partial refund (configurable %)
  - After production start: no automatic refund (manager override only)

Response 200:
{
  "refundCents":  7100,
  "refundMethod": "original_card",
  "status":       "cancelled"
}
```

---

## Occasion Gift Message Suggestions (CX-05)

#### `GET /api/v1/storefront/gift-messages`
```
Query: occasion=birthday|anniversary|thank_you|new_baby|sympathy

Response 200:
{
  "suggestions": [
    "Happy birthday — eat the flowers.",
    "Congratulations. Thoroughly deserved.",
    "Thinking of you today.",
    "Thank you, genuinely."
  ]
}
```
