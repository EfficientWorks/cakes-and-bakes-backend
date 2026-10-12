-- +goose Up

-- Swap the payment provider from Stripe to Square (ADR: replace, not
-- dual-run). Square's Payments API identifies a captured payment by a
-- single `payment.id` — there is no separate "charge id" the way Stripe has
-- PaymentIntent + Charge, so the two stripe_* id columns collapse into one.
-- square_payment_method_id keeps the "how it was tokenised" reference (the
-- Web Payments SDK source_id / nonce), matching what stripe_payment_method_id
-- held before.

ALTER TABLE payments RENAME COLUMN stripe_payment_intent_id TO square_payment_id;
ALTER TABLE payments RENAME COLUMN stripe_payment_method_id TO square_payment_method_id;
ALTER TABLE payments DROP COLUMN stripe_charge_id;

ALTER TABLE refunds RENAME COLUMN stripe_refund_id TO square_refund_id;

ALTER TABLE orders RENAME COLUMN stripe_payment_intent_id TO square_payment_id;

-- Indexes were named after the old column; recreate under the new name so a
-- fresh read of \d payments doesn't say "stripe".
DROP INDEX IF EXISTS idx_payments_stripe_intent;
CREATE UNIQUE INDEX idx_payments_square_payment ON payments (square_payment_id) WHERE square_payment_id IS NOT NULL;

-- Tracks hosted Square Payment Links created for storefront checkout.
--
-- Square, unlike Stripe Checkout, does not substitute anything into
-- checkout_options.redirect_url — there is no {CHECKOUT_SESSION_ID}-style
-- placeholder. So the id the storefront gets back and later reads back with
-- (GetCheckoutSession) is one this backend mints itself, before the Square
-- API call, and embeds in the redirect URL query string. This table is the
-- lookup from that id to what Square actually created.
CREATE TABLE square_checkout_sessions (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    square_payment_link_id TEXT NOT NULL,
    square_order_id   TEXT NOT NULL,
    amount_cents      INTEGER NOT NULL,
    currency          TEXT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down

DROP TABLE IF EXISTS square_checkout_sessions;

DROP INDEX IF EXISTS idx_payments_square_payment;
CREATE UNIQUE INDEX idx_payments_stripe_intent ON payments (square_payment_id) WHERE square_payment_id IS NOT NULL;

ALTER TABLE orders RENAME COLUMN square_payment_id TO stripe_payment_intent_id;

ALTER TABLE refunds RENAME COLUMN square_refund_id TO stripe_refund_id;

ALTER TABLE payments ADD COLUMN stripe_charge_id TEXT;
ALTER TABLE payments RENAME COLUMN square_payment_method_id TO stripe_payment_method_id;
ALTER TABLE payments RENAME COLUMN square_payment_id TO stripe_payment_intent_id;
