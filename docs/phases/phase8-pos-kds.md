# Phase 8 — POS + KDS

This phase adds POS-specific and KDS-specific flows on top of the existing `orders`,
`scheduling`, and `payments` services. No new service. New endpoints and extensions only.

Target: R2 — 9 September 2026.

## 8.1 POS order flow additions

POS handles 3 order types: **takeaway** (walk-in), **pickup** (with scheduled time), **delivery**.

All 3 use the same `POST /v1/orders` endpoint as the storefront, with `channel: "pos"`.

**Differences from storefront flow:**
- Auth token: `staff` realm, `branch_id` in claims (device is pinned to one branch).
- Slot picker: counter staff picks from available slots, same `GET /v1/scheduling/slots`.
- Payment: Stripe Terminal card OR cash (not Stripe Elements).
- Receipt: triggers print job via Print Bridge.

**New POS-specific endpoints:**

```
POST   /v1/pos/orders                         → create walk-in order (shortcut: no routing)
GET    /v1/pos/orders/today                   → today's orders for this branch (order screen)
GET    /v1/pos/orders/history                 → past orders (searchable)
GET    /v1/pos/orders/scheduled               → scheduled future orders
GET    /v1/pos/orders/online                  → online orders from storefront (accept/reject)
POST   /v1/pos/orders/{id}/accept-online      → accept an incoming online order
POST   /v1/pos/orders/{id}/reject-online      → reject with reason

POST   /v1/pos/shift/open                     → start shift (staff login already done)
POST   /v1/pos/shift/close                    → end shift, submit cash count
GET    /v1/pos/shift/current                  → current shift summary (orders taken, cash)

POST   /v1/pos/reports/daily                  → daily report for this branch
POST   /v1/pos/reports/payin                  → record cash float addition
POST   /v1/pos/reports/payout                 → record cash removal

POST   /v1/pos/day-close                      → process Z-sale, submit end-of-day
```

## 8.2 KDS flow

KDS is an `orders` service consumer. KDS Flutter app uses:

```
GET  /v1/kds/today?branch_id=X     → production board for today
GET  /v1/kds/tomorrow?branch_id=X  → aggregated plan (item counts by product)
GET  /v1/kds/week?branch_id=X      → 7-day volume overview

POST /v1/orders/{id}/items/{itemId}/start  → mark item in-progress
POST /v1/orders/{id}/items/{itemId}/done   → mark item done
```

KDS receives all `order.*` and `order.item.*` events via SSE subscription.

**KDS display rules:**
- `IN_PRODUCTION` items: show with timer (time since `prep_started_at`).
- `CANCELLED` orders: remain on board with `[CANCELLED]` label, never disappear.
- `HOLD` items: flagged with `[HOLD]` (branch manager can hold an item — Phase 2).
- Urgency: computed server-side — green (>2h to slot), yellow (30min–2h), red (<30min).
- Recipient name is the largest element in each order card.
- Gift message displayed prominently below recipient.

## 8.3 Shift management DB additions

```sql
-- Shift records
shifts (
  id UUID PRIMARY KEY,
  branch_id UUID NOT NULL,
  opened_by_user_id UUID NOT NULL,
  closed_by_user_id UUID,
  opened_at TIMESTAMPTZ NOT NULL,
  closed_at TIMESTAMPTZ,
  opening_cash_cents INTEGER,
  closing_cash_cents INTEGER,
  expected_cash_cents INTEGER,  -- computed from orders
  variance_cents INTEGER,
  status VARCHAR DEFAULT 'open'  -- 'open' | 'closed'
)

-- Cash movements during shift
cash_movements (
  id, shift_id, type VARCHAR,  -- 'payin' | 'payout' | 'starting_float'
  amount_cents INTEGER NOT NULL,
  reason TEXT,
  recorded_by_user_id UUID NOT NULL,
  created_at TIMESTAMPTZ
)
```

## 8.4 Print Bridge integration

The Print Bridge is a Node.js sidecar running on the Windows POS device at localhost:9100.
The Go backend does not talk to the printer directly.

```
(KDS/POS Flutter app) → POST localhost:9100/v1/print → Print Bridge → USB printer
```

The POS Flutter app calls the Print Bridge directly for receipts and KOT tickets.
The backend does not need to manage printing — it just emits order events.

**Print ticket layout (KDS-13 — the critical fix for the Kajal pain point):**

```
[LARGE] RECIPIENT: SARAH JOHNSON
[MEDIUM] Gift for: Sarah Johnson
[SMALL] Ref: BB-001234
━━━━━━━━━━━━━━━━━━━━━━━━━━
Delivery: Friday 5 Sep, 2:00–3:00PM
Edison NJ Branch
━━━━━━━━━━━━━━━━━━━━━━━━━━
1× Summer Sunset Bouquet (10 cupcakes)
   Flavour: Vanilla + Strawberry
   Design: Tropical Sunset
1× Butterfly Topper Add-on
━━━━━━━━━━━━━━━━━━━━━━━━━━
🎂 Gift message: Happy birthday beautiful!
   We'll letter this onto the card.
━━━━━━━━━━━━━━━━━━━━━━━━━━
[TINY] Purchaser: Tina M. | tina@email.com
```

Recipient name is printed large. Purchaser is at the bottom in small text. This directly
fixes the misdelivery problem (BRD P2).

## 8.5 Auth for POS/KDS (terminal login — Phase 1.4)

```
POST /v1/auth/terminal/login
{
  "branch_registry_id": "BB-NJ-001",
  "pin": "1234",
  "app": "pos"           // or "kds"
}
```

PIN lookup uses indexed HMAC-SHA256 hash (`pin_lookup` column) to find the candidate
user in O(1) without scanning all staff. Then argon2id-verifies the PIN against the
full hash. This is the same mechanism as emzo-restaurant passcode login.

App access gate: `kitchen_staff` can only log in to `app: "kds"`. `counter_staff` and
`branch_manager` can log in to `app: "pos"`.

## Previous / Next

← [Phase 7 — Realtime](phase7-realtime.md) · [Phase 9 — Gateway](phase9-gateway.md) →
