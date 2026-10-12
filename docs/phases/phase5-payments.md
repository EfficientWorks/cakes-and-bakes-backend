# Phase 5 — Payments

Service: `services/payments/`. See `docs/modules/payments/README.md`.

Stripe-first from day one (unlike emzo-restaurant which used manual entry first).
Online payments (storefront) are required for R1. Stripe Terminal (POS) for R2.

## 5.1 DB migrations

```sql
-- Payment intents (one per order, may have multiple attempts)
payment_intents (
  id UUID PRIMARY KEY,
  order_id UUID NOT NULL,
  stripe_payment_intent_id VARCHAR UNIQUE,
  amount_cents INTEGER NOT NULL,
  currency VARCHAR(3) DEFAULT 'USD',
  status VARCHAR,              -- 'pending' | 'succeeded' | 'failed' | 'cancelled'
  payment_method_type VARCHAR, -- 'card' | 'apple_pay' | 'google_pay' | 'stripe_terminal'
  stripe_charge_id VARCHAR,
  failure_code VARCHAR,
  failure_message VARCHAR,
  idempotency_key VARCHAR UNIQUE NOT NULL,
  created_at TIMESTAMPTZ, updated_at TIMESTAMPTZ
)

-- Refunds
refunds (
  id UUID PRIMARY KEY,
  order_id UUID NOT NULL,
  payment_intent_id UUID NOT NULL,
  stripe_refund_id VARCHAR UNIQUE,
  amount_cents INTEGER NOT NULL,
  currency VARCHAR(3) DEFAULT 'USD',
  reason VARCHAR,
  status VARCHAR,              -- 'pending' | 'succeeded' | 'failed'
  issued_by_actor_id UUID NOT NULL,
  approved_by_actor_id UUID,   -- for refunds above per-user limit
  created_at TIMESTAMPTZ, updated_at TIMESTAMPTZ
)

-- Stripe webhook events (idempotent processing)
stripe_webhook_events (
  id UUID PRIMARY KEY,
  stripe_event_id VARCHAR UNIQUE NOT NULL,  -- prevents double-processing
  event_type VARCHAR NOT NULL,
  payload JSONB NOT NULL,
  processed_at TIMESTAMPTZ,
  processing_error TEXT,
  created_at TIMESTAMPTZ
)

-- POS terminal readers (R2)
terminal_readers (
  id UUID PRIMARY KEY,
  branch_id UUID NOT NULL,
  stripe_reader_id VARCHAR UNIQUE,
  label VARCHAR,
  status VARCHAR,              -- 'active' | 'offline' | 'updating'
  last_seen_at TIMESTAMPTZ,
  created_at
)

-- POS cash transactions (R2 — cash not via Stripe but recorded here)
cash_transactions (
  id UUID PRIMARY KEY,
  order_id UUID NOT NULL,
  branch_id UUID NOT NULL,
  amount_cents INTEGER NOT NULL,
  currency VARCHAR(3) DEFAULT 'USD',
  change_given_cents INTEGER DEFAULT 0,
  cash_tendered_cents INTEGER NOT NULL,
  recorded_by_actor_id UUID NOT NULL,
  created_at TIMESTAMPTZ
)
```

## 5.2 Stripe online payment flow (R1 — storefront)

```
1. Client: POST /v1/orders → server returns {order_id, payment_intent_client_secret}
2. Client: Stripe Elements charges the card using client_secret
3. Stripe → POST /v1/webhooks/stripe (payment_intent.succeeded)
4. Server: mark order status = 'confirmed', emit order.confirmed event
5. Server: send confirmation email (via email service / Postmark)
```

**Stripe idempotency**: Payment Intent is created with `idempotency_key = order.idempotency_key`.
If the server crashes after creating the intent but before responding, the retry returns
the same intent.

**3D Secure**: Stripe Radar handles automatically. High-risk transactions require 3DS.

## 5.3 Stripe webhook handler

```
POST /v1/webhooks/stripe
```

Verified via `Stripe-Signature` header + webhook secret.
Processed idempotently via `stripe_webhook_events.stripe_event_id` unique constraint.

Handled events:

| Event | Action |
|---|---|
| `payment_intent.succeeded` | Mark order `confirmed`, emit event, send email |
| `payment_intent.payment_failed` | Mark order payment failed, notify admin |
| `charge.dispute.created` | Alert admin, log dispute for evidence response |
| `refund.updated` | Sync refund status |

## 5.4 Refund flow

```
POST /v1/payments/orders/{orderId}/refund
{
  "amount_cents": 8800,    // partial or full
  "reason": "customer_requested"
}
```

Permission check: `RBAC-03` — refund limited by `numeric_limits.max_refund_cents` per user.
Manager override flow for above-limit refunds (RBAC-04, Phase 2).

Stripe API call: `stripe.Refunds.Create({charge: X, amount: Y})`.
On `refund.updated` webhook: update `refunds.status`.

## 5.5 Stripe Terminal (R2 — POS)

POS Flutter app uses Stripe Terminal SDK. Server provides connection token.

```
POST /v1/payments/terminal/connection-token   → {secret} (Stripe connection token)
POST /v1/payments/terminal/create-intent      → creates PaymentIntent for terminal
POST /v1/payments/terminal/capture/{id}       → capture after reader confirms
```

Split payment (RBAC: counter staff can do this):
- `POST /v1/payments/pos/orders/{id}/split-payment` — record cash portion + card portion.

Cash payment (POS):
- `POST /v1/payments/pos/orders/{id}/cash` — record cash amount, calculate change.
- Triggers cash drawer kick event to POS app.

## 5.6 Discount codes

```sql
-- discount_codes table (owned by admin/franchise service, read here for validation)
-- This service validates on order creation, not owns the codes
```

Validation at order creation: `POST /v1/payments/discount-codes/validate`.
- Returns `{discount_type, discount_value_cents, is_valid, error_code}`.
- Decrement `times_used` atomically inside order creation transaction.

Supported discount types: `fixed_amount` | `percentage` | `free_delivery`.

## 5.7 Tip handling (R2 — POS)

- Tip added at POS payment step (optional).
- Stored in `orders.tip_cents`. Not refunded on normal refunds.
- Tip cannot be added after payment on web.

## 5.8 Swagger + tests

`services/payments/api/openapi.yaml`. `postman/payments.postman_collection.json`.
Integration tests: Payment Intent creation, Stripe webhook processing (mocked),
refund creation, duplicate webhook event handling (idempotent).

## Previous / Next

← [Phase 4 — Orders + Scheduling](phase4-orders.md) · [Phase 6 — Admin](phase6-admin.md) →
