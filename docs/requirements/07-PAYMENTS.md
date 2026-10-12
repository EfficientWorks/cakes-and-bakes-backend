# Module 07 — Payments, Refunds & Tax

**Phase:** R1 (online), R2 (POS/Terminal)  
**BRD refs:** PAY-01 to PAY-09  
**Surfaces:** Storefront checkout, branch admin, super admin

---

## Overview

- Online payments via Stripe Elements + Apple Pay + Google Pay
- No raw card data ever touches the platform (PAY-09 — PCI SAQ-A)
- All amounts in USD cents (integers)
- Tax configured per branch (multi-state from day one)

---

## Database Tables

```sql
payments (
  id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id                UUID NOT NULL REFERENCES orders(id),
  franchise_id            UUID NOT NULL,
  branch_id               UUID NOT NULL,
  method                  TEXT NOT NULL,     -- card | cash | apple_pay | google_pay | terminal
  amount_cents            INTEGER NOT NULL,
  stripe_payment_intent_id TEXT,
  stripe_charge_id         TEXT,
  stripe_payment_method_id TEXT,            -- tokenised, never raw card data
  status                  TEXT NOT NULL DEFAULT 'pending',
                          -- pending | captured | failed | refunded | partial_refund
  captured_at             TIMESTAMPTZ,
  created_at              TIMESTAMPTZ DEFAULT NOW()
)

refunds (
  id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  payment_id            UUID NOT NULL REFERENCES payments(id),
  order_id              UUID NOT NULL REFERENCES orders(id),
  amount_cents          INTEGER NOT NULL,
  reason                TEXT NOT NULL,
  stripe_refund_id      TEXT,
  status                TEXT DEFAULT 'pending',  -- pending | succeeded | failed
  issued_by_user_id     UUID NOT NULL,
  approved_by_user_id   UUID,                    -- for amounts above user limit
  created_at            TIMESTAMPTZ DEFAULT NOW()
)

tax_rates (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  branch_id       UUID NOT NULL REFERENCES branches(id),
  state           TEXT NOT NULL,
  county          TEXT,
  rate_pct        DECIMAL(6,4) NOT NULL,    -- e.g. 0.06625 for 6.625%
  tax_label       TEXT,                     -- "NJ Sales Tax"
  is_active       BOOLEAN DEFAULT true,
  effective_from  DATE NOT NULL,
  created_at      TIMESTAMPTZ DEFAULT NOW()
)
```

---

## Payment Flow

### Step 1: Create Payment Intent

#### `POST /api/v1/storefront/payments/intent`
```
Called before showing the payment form.

Body:
{
  "cartId": "..."
}

Server-side:
  1. Calculate final total server-side (never trust client price)
  2. Create Stripe PaymentIntent with amount, currency, metadata
  3. Return client_secret for Stripe Elements

Response 200:
{
  "clientSecret":   "pi_xxx_secret_xxx",
  "amount":         36888,
  "currency":       "USD",
  "paymentMethods": ["card", "apple_pay", "google_pay"]
}
```

### Step 2: Confirm Payment (at order creation)

Payment confirmation happens inside `POST /api/v1/storefront/orders`.
The Stripe PaymentMethod is passed in, the server:
1. Confirms the PaymentIntent
2. Only creates the order record AFTER successful capture
3. Atomic: order + payment + slot reservation in one transaction

---

## Stripe Webhook Handler

#### `POST /api/v1/webhooks/stripe`
```
Stripe-Signature header verified against webhook secret.

Handled events:
  payment_intent.succeeded      → update payment status to captured
  payment_intent.payment_failed → update payment status to failed, notify staff
  charge.refunded               → update refund status to succeeded
  charge.dispute.created        → flag order, notify super admin

Response: 200 always (Stripe retries on non-2xx)
```

---

## Refunds

#### `POST /api/v1/admin/orders/:orderId/refund`
```
Auth: staff token with refund permission (subject to numeric limit RBAC-03)

Body:
{
  "amountCents": 7100,           // partial or full
  "reason":      "Customer cancellation — before cut-off"
}

Server-side:
  1. Check amount <= payment amount
  2. Check actor's refund limit (RBAC numeric limits)
  3. If exceeds limit: require approver PIN (Phase 2, RBAC-04)
  4. Create Stripe refund
  5. Write refunds record
  6. Update order.payment_status
  7. Write audit_log

Response 200:
{
  "refundId":    "...",
  "amountCents": 7100,
  "status":      "succeeded",
  "stripeRefundId": "re_xxx"
}

Errors:
  403 REFUND_LIMIT_EXCEEDED — { "yourLimit": 5000, "requested": 7100 }
  409 ALREADY_FULLY_REFUNDED
  409 REFUND_EXCEEDS_ORIGINAL_PAYMENT
```

---

## Tax Calculation

```typescript
function calculateTax(
  subtotalCents: number,
  branchId: string,
): TaxResult {
  const rate = await getActiveTaxRate(branchId);  // from tax_rates table

  return {
    ratePct:    rate.ratePct,              // e.g. 0.06625
    taxLabel:   rate.taxLabel,             // "Estimated taxes (Edison, NJ)"
    taxCents:   Math.round(subtotalCents * rate.ratePct),
  };
}
// Displayed on storefront as: "Estimated taxes (Edison, NJ) ⓘ  $22.92"
```

### Tax Rate Management (Super Admin)

#### `GET /api/v1/admin/branches/:branchId/tax`
#### `POST /api/v1/admin/branches/:branchId/tax`
```
Body:
{
  "state":        "NJ",
  "county":       "Middlesex",
  "ratePct":      0.06625,
  "taxLabel":     "NJ Sales Tax",
  "effectiveFrom": "2026-01-01"
}
```

---

## Price Summary Object (used across all APIs)

```typescript
interface PriceSummary {
  subtotalCents:    number;   // sum of line items before discounts
  discountCents:    number;   // 0 if no code applied
  deliveryFeeCents: number;   // 0 = free delivery
  taxCents:         number;
  tipCents:         number;   // 0 if no tip
  totalCents:       number;   // subtotal - discount + delivery + tax + tip
  currency:         'USD';
  taxLabel:         string;   // e.g. "Estimated taxes (Edison, NJ)"
  freeDeliveryNote: string | null; // e.g. "Free delivery over $75"
}
```

---

## Delivery Fee Logic

```typescript
// From Figma cart: "We deliver to this ZIP — free over $75"
// From BRD: delivery fee per zone, minimum order

function calculateDeliveryFee(
  branchDeliveryFeeCents: number,   // e.g. 3000 = $30
  subtotalCents: number,
  minOrderForFreeDeliveryCents: number = 7500  // $75
): { feeCents: number; freeDeliveryNote: string | null } {

  if (subtotalCents >= minOrderForFreeDeliveryCents) {
    return {
      feeCents: 0,
      freeDeliveryNote: `Free delivery over $${minOrderForFreeDeliveryCents / 100}`,
    };
  }

  return {
    feeCents: branchDeliveryFeeCents,
    freeDeliveryNote: null,
  };
}
```
