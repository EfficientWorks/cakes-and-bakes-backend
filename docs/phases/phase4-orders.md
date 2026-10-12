# Phase 4 — Orders + Scheduling

Services: `services/orders/` and `services/scheduling/`.
See `docs/modules/orders/README.md` and `docs/modules/scheduling/README.md`.

This is the hottest path in the system. Everything that matters operationally goes through here.
The outbox pattern is wired here for the first time.

## Architecture note

`scheduling` may be implemented as an `internal/scheduling/` module inside `orders`
rather than a fully separate service — they share the same database transaction boundary
(slot reservation + order creation must be atomic). Decide at build time based on
coupling. This file treats them as co-located modules.

---

# Part A — Scheduling

## A.1 DB migrations (scheduling)

```sql
-- Slot rules per branch
slot_rules (
  id, branch_id,
  day_of_week INTEGER,        -- 0=Sunday … 6=Saturday, null = applies all days
  slot_start TIME,
  slot_duration_minutes INTEGER,
  max_orders_per_slot INTEGER,
  is_active BOOLEAN DEFAULT true,
  created_at, updated_at
)

-- Cut-off rules per branch
cutoff_rules (
  id, branch_id,
  order_type VARCHAR,          -- 'pickup' | 'delivery' | 'all'
  cutoff_hours_before INTEGER, -- hours before slot start
  same_day_cutoff TIME,        -- e.g. 14:00 = no same-day orders after 2pm
  created_at, updated_at
)

-- Exception dates (blackouts, special capacity)
exception_dates (
  id, branch_id,
  exception_date DATE,
  type VARCHAR,                -- 'closed' | 'reduced_capacity' | 'extended_hours'
  max_orders_override INTEGER, -- null = closed
  reason VARCHAR,
  created_at
)

-- Slot inventory (live capacity tracking)
-- One row per branch × date × slot_start
slot_inventory (
  id, branch_id, slot_date DATE, slot_start TIME,
  total_capacity INTEGER,
  orders_count INTEGER DEFAULT 0,
  UNIQUE (branch_id, slot_date, slot_start)
)
```

## A.2 Slot availability algorithm

Called when storefront requests available slots for a branch + date range.

```
1. Generate candidate slots from slot_rules for the requested dates.
2. Filter against cutoff_rules (remove past-cutoff slots).
3. Filter against exception_dates (remove closed dates, apply reduced capacity).
4. Join against slot_inventory to get remaining capacity.
5. Remove slots with orders_count >= total_capacity.
6. Return sorted list with remaining capacity per slot.
```

Performance: this must complete in < 50ms. Candidate slots are generated in Go (not SQL),
then batch-joined against slot_inventory in one query.

## A.3 Slot reservation (atomic)

```sql
-- Inside orders service transaction:
UPDATE slot_inventory
SET orders_count = orders_count + 1
WHERE branch_id = $1
  AND slot_date = $2
  AND slot_start = $3
  AND orders_count < total_capacity
RETURNING id;
-- If 0 rows returned: slot is full → return SLOT_CAPACITY_EXCEEDED error
-- Order creation continues only if this UPDATE succeeds
```

## A.4 API

```
GET /v1/scheduling/slots?branch_id=X&date_from=Y&date_to=Z&order_type=pickup
→ available slots with remaining capacity

GET /v1/scheduling/cutoff?branch_id=X
→ cut-off times and same-day cut-off for display (SF-07)

GET /v1/scheduling/exceptions?branch_id=X&month=2026-09
→ exception dates for calendar UI
```

---

# Part B — Orders

## B.1 DB migrations (orders)

```sql
-- Core orders table
orders (
  id UUID PRIMARY KEY,
  -- Immutable identifiers (set at creation, never changed)
  brand_id UUID NOT NULL,
  franchise_id UUID NOT NULL,
  branch_id UUID NOT NULL,
  -- Order reference (customer-visible)
  order_number VARCHAR UNIQUE,    -- e.g. 'BB-001234'
  -- Channel
  channel VARCHAR NOT NULL,       -- 'storefront' | 'pos' | 'phone'
  -- Customer
  customer_id UUID,               -- null for guest orders
  purchaser_first_name VARCHAR NOT NULL,
  purchaser_last_name VARCHAR NOT NULL,
  purchaser_email VARCHAR NOT NULL,
  purchaser_phone VARCHAR NOT NULL,
  -- Recipient (the person receiving the gift)
  recipient_first_name VARCHAR NOT NULL,
  recipient_last_name VARCHAR NOT NULL,
  recipient_phone VARCHAR,        -- required for delivery
  gift_message VARCHAR(120),
  is_gift BOOLEAN DEFAULT false,
  -- Fulfilment
  fulfilment_type VARCHAR NOT NULL,  -- 'pickup' | 'delivery'
  scheduled_date DATE NOT NULL,
  slot_start TIME NOT NULL,
  slot_end TIME NOT NULL,
  -- Delivery (null for pickup)
  delivery_address JSONB,
  delivery_instructions TEXT,
  delivery_fee_cents INTEGER DEFAULT 0,
  -- Pricing
  subtotal_cents INTEGER NOT NULL,
  discount_cents INTEGER DEFAULT 0,
  tax_cents INTEGER NOT NULL,
  tip_cents INTEGER DEFAULT 0,
  total_cents INTEGER NOT NULL,
  currency VARCHAR(3) DEFAULT 'USD',
  -- Discount
  discount_code VARCHAR,
  -- Status
  status VARCHAR NOT NULL DEFAULT 'placed',
  -- 'placed' | 'confirmed' | 'scheduled' | 'in_production' | 'ready' | 'fulfilled' | 'cancelled'
  -- Cancellation
  cancelled_at TIMESTAMPTZ,
  cancel_reason VARCHAR,
  cancelled_by_actor_id UUID,
  -- Amendment
  last_amended_at TIMESTAMPTZ,
  last_amended_by_actor_id UUID,
  -- Idempotency
  idempotency_key VARCHAR UNIQUE NOT NULL,
  -- Audit
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  -- Trading date (branch-local date for reporting)
  trading_date DATE NOT NULL
)

-- Order items (one row per product configured)
order_items (
  id UUID PRIMARY KEY,
  order_id UUID NOT NULL REFERENCES orders(id),
  product_id UUID NOT NULL,
  product_name VARCHAR NOT NULL,   -- denormalised — survives product deletion
  variant_id UUID,
  variant_name VARCHAR,
  unit_price_cents INTEGER NOT NULL,
  quantity INTEGER DEFAULT 1,
  -- Flavour selections (JSONB array)
  flavour_selections JSONB,        -- [{id, name}]
  -- Add-on selections (JSONB array)
  addon_selections JSONB,          -- [{id, name, price_cents}]
  -- Configuration snapshot (denormalised for receipt/audit)
  config_snapshot JSONB,
  -- KDS production status
  prep_status VARCHAR DEFAULT 'pending',
  -- 'pending' | 'in_progress' | 'done'
  prep_started_at TIMESTAMPTZ,
  prep_completed_at TIMESTAMPTZ,
  prep_by_user_id UUID,
  created_at TIMESTAMPTZ
)

-- Order status history (full audit trail)
order_status_history (
  id, order_id, previous_status, new_status,
  actor_id, actor_role, reason TEXT,
  created_at TIMESTAMPTZ
)

-- Order amendments
order_amendments (
  id, order_id, amendment_type VARCHAR,
  -- 'recipient_phone' | 'gift_message' | 'delivery_instructions' |
  -- 'slot_change' | 'item_change' | 'cancel'
  field_changed VARCHAR, old_value JSONB, new_value JSONB,
  price_delta_cents INTEGER DEFAULT 0,
  actor_id UUID, actor_role VARCHAR,
  stripe_payment_intent_id VARCHAR,  -- if delta required payment capture/refund
  created_at TIMESTAMPTZ
)

-- Outbox (domain events — same transaction as state change)
orders_outbox (
  id UUID, event_type VARCHAR, payload JSONB,
  aggregate_id UUID, aggregate_type VARCHAR,
  published_at TIMESTAMPTZ, created_at TIMESTAMPTZ
)

-- Exception queue (orders that failed to route — ORD-05)
routing_exceptions (
  id, zip_code VARCHAR, requested_date DATE,
  requested_fulfilment_type VARCHAR, reason VARCHAR,
  status VARCHAR DEFAULT 'pending',  -- 'pending' | 'assigned' | 'resolved'
  assigned_branch_id UUID,
  resolved_at TIMESTAMPTZ, created_at TIMESTAMPTZ
)

-- Guest order claim tokens (ACC-14 — Phase 2)
guest_order_tokens (
  id, order_id, token_hash VARCHAR, email VARCHAR,
  expires_at TIMESTAMPTZ, used_at TIMESTAMPTZ, created_at TIMESTAMPTZ
)
```

## B.2 Routing engine

Called at checkout, before payment is taken.

```go
// internal/routing/router.go

func (r *Router) Resolve(ctx context.Context, req RoutingRequest) (*BranchMatch, error) {
  switch req.FulfilmentType {
  case "pickup":
    return r.resolvePickup(ctx, req)      // explicit branch selection
  case "delivery":
    return r.resolveDelivery(ctx, req)    // zone match + tie-break
  }
}

func (r *Router) resolveDelivery(ctx context.Context, req RoutingRequest) (*BranchMatch, error) {
  // 1. Geocode the delivery address → lat/lng
  // 2. PostGIS: find branches whose delivery_zone contains the point
  //    SELECT b.* FROM branches b
  //    WHERE ST_Contains(b.delivery_zone, ST_MakePoint($lng, $lat))
  //      AND b.status = 'active'
  // 3. If 0 matches → add to routing_exceptions, return DELIVERY_NOT_AVAILABLE
  // 4. If 1 match → return that branch
  // 5. If >1 matches (overlap) → apply tie-break:
  //    a. Nearest by road distance (Google Maps Distance Matrix API)
  //    b. Most remaining slot capacity for requested window
  //    c. Branch code (alphabetical)
  // 6. Check resolved branch has slot capacity for requested window
  // 7. Return BranchMatch{BranchID, DeliveryFeeCents}
}
```

## B.3 Order creation (storefront flow)

```
POST /v1/orders
{
  "idempotency_key": "uuid",
  "fulfilment_type": "delivery",
  "branch_id": "uuid",          // resolved by routing, passed from client
  "franchise_id": "uuid",
  "scheduled_date": "2026-09-05",
  "slot_start": "14:00",
  "purchaser": {...},
  "recipient": {...},
  "items": [{
    "product_id": "uuid",
    "variant_id": "uuid",
    "flavour_selections": ["uuid"],
    "addon_selections": ["uuid"],
    "quantity": 1
  }],
  "delivery_address": {...},
  "delivery_instructions": "...",
  "discount_code": "SUMMER20",
  "gift_message": "Happy Birthday!"
}
```

**Server-side order creation steps** (all in one transaction):

1. Check idempotency key — if exists, return existing order.
2. Verify branch is active and can fulfil the requested slot.
3. Call catalog service (ConnectRPC) to validate all items are available.
4. Apply discount code if present — validate, decrement use count.
5. Calculate pricing: subtotal + delivery fee + tax + discount.
6. Atomically decrement slot capacity (`slot_inventory`).
7. Create `orders` record.
8. Create `order_items` records.
9. Write to `orders_outbox` (`order.routed` event).
10. Commit transaction.
11. (Async) Create Stripe Payment Intent, return `{order_id, client_secret}`.

Payment is captured separately after the customer submits card details.
Order status moves to `placed` only when Stripe confirms payment (`payment.succeeded` webhook).

## B.4 Order amendment rules (BRD ORD section)

**Amendment window**: while order status is `placed`, `confirmed`, or `scheduled`.
Amendment window closes when status moves to `in_production`.

| Amendment type | Who can do it | Notes |
|---|---|---|
| Recipient phone | Customer, branch manager | No price impact |
| Gift message | Customer, branch manager | No price impact |
| Delivery instructions | Customer, branch manager | No price impact |
| Slot change | Branch manager only | Release old slot capacity, reserve new slot |
| Item change | Branch manager only | Recalculate price, capture/refund delta via Stripe |
| Cancel | Customer (within window), branch manager | Full refund if before cut-off |

```
PATCH /v1/orders/{id}/recipient-phone
PATCH /v1/orders/{id}/gift-message
PATCH /v1/orders/{id}/delivery-instructions
PATCH /v1/orders/{id}/slot        (branch manager only)
PATCH /v1/orders/{id}/items       (branch manager only)
POST  /v1/orders/{id}/cancel
```

## B.5 Order management API

```
GET    /v1/orders                           → branch order list (filtered by date, status, type)
GET    /v1/orders/{id}                      → order detail with items and history
POST   /v1/orders/{id}/confirm              → manual confirm (R1 — branch manager)
POST   /v1/orders/{id}/mark-in-production   → production started (by KDS)
POST   /v1/orders/{id}/mark-ready           → all items done (auto from KDS)
POST   /v1/orders/{id}/mark-fulfilled       → collected/delivered
POST   /v1/orders/{id}/items/{itemId}/start → item in-progress (KDS)
POST   /v1/orders/{id}/items/{itemId}/done  → item done (KDS)
POST   /v1/orders/{id}/refund               → refund (within limits)
```

## B.6 Tracking page (public — no auth)

```
GET /v1/track/{secureToken}   → public order status (SF-32)
```

`secureToken` = HMAC-SHA256 of `order_id` with a server secret. Stateless, no DB lookup.
Returns: order reference, recipient first name only, status, fulfilment window, branch name.
Does not expose purchaser details or pricing to unauthenticated callers.

## B.7 Exception queue API (super admin)

```
GET   /v1/admin/routing-exceptions              → unrouted orders (SA-14)
POST  /v1/admin/routing-exceptions/{id}/assign  → manually assign to branch
```

## B.8 Outbox events emitted

| Event | Trigger | Consumers |
|---|---|---|
| `order.routed` | Order created, branch assigned | `realtime` → KDS push, `admin` → dashboard |
| `order.confirmed` | Branch confirms | `realtime` → storefront tracking, email |
| `order.status_changed` | Any status transition | `realtime` → KDS, tracking page |
| `order.item.started` | Item marked in_progress | `realtime` → KDS |
| `order.item.done` | Item marked done | `realtime` → KDS |
| `order.ready` | All items done (auto) | `realtime` → branch admin, tracking |
| `order.fulfilled` | Handed over | `realtime` → tracking, admin |
| `order.cancelled` | Cancellation | `realtime` → KDS (remove), admin, email |
| `order.amended` | Any amendment | `realtime` → KDS (highlight changed fields) |
| `slot.capacity_changed` | Slot inventory update | `realtime` → storefront slot picker |

## B.9 KDS-specific flows (integrated in orders service)

The KDS Flutter app subscribes to the branch SSE channel. On connection:
1. `GET /v1/kds/today` — orders for today's production, sorted by slot time.
2. `GET /v1/kds/tomorrow` — aggregated item counts for tomorrow (production planning).
3. `GET /v1/kds/week` — 7-day volume summary (staffing/ingredient planning).
4. Receives real-time updates via SSE for all `order.*` and `order.item.*` events.

```
GET /v1/kds/today?branch_id=X
GET /v1/kds/tomorrow?branch_id=X
GET /v1/kds/week?branch_id=X
```

KDS today view response includes:
- Orders grouped by slot (e.g. 14:00–14:30)
- Per order: items with prep status, recipient name (prominent), gift message
- Urgency: computed field based on minutes to slot time (green/yellow/red)
- CANCELLED and HOLD items flagged but still visible (not removed from board)

## B.10 Idempotency

Order creation requires client-generated `idempotency_key` (UUID). If the same key arrives
a second time (network retry), the server returns the existing order without re-processing.
Payment attempts also carry Stripe idempotency keys.

## B.11 Tests

Integration:
- Full order creation round-trip including slot reservation.
- Slot overbooking: 10 concurrent order requests for a slot with capacity 9 — exactly 9
  succeed, 1 returns `SLOT_CAPACITY_EXCEEDED`.
- Amendment: slot change releases capacity at old slot, reserves at new slot.
- Idempotency: same key submitted twice returns same response, no duplicate order.
- Routing: delivery address in zone A routes to branch A; address in overlap routes to
  nearer branch.

## Previous / Next

← [Phase 3 — Catalog](phase3-catalog.md) · [Phase 5 — Payments](phase5-payments.md) →
