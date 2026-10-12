-- +goose Up
-- +goose StatementBegin

/*
 * Square is removed from the product; Stripe takes every card payment.
 *
 * 0036 renamed the Stripe payment columns to square_* when Square was added,
 * and since then they have held whichever provider's id took the money. They
 * get provider-neutral names here, and everything that existed only for
 * Square goes: the checkout-session lookup, the Square order id on payment
 * links, and 'square' as an allowed provider.
 *
 * The provider checks are added without a fallback for existing 'square'
 * rows on purpose: this migration is for a database that never took Square
 * payments, and one that did should fail here rather than relabel money.
 */
DROP TABLE IF EXISTS square_checkout_sessions;

ALTER TABLE payments RENAME COLUMN square_payment_id TO provider_payment_id;
ALTER TABLE payments RENAME COLUMN square_payment_method_id TO provider_payment_method_id;
ALTER INDEX IF EXISTS idx_payments_square_payment RENAME TO idx_payments_provider_payment;
ALTER TABLE refunds RENAME COLUMN square_refund_id TO provider_refund_id;
ALTER TABLE orders RENAME COLUMN square_payment_id TO provider_payment_id;

DROP INDEX IF EXISTS payment_links_square_order_idx;
ALTER TABLE payment_links DROP COLUMN IF EXISTS provider_order_id;
ALTER TABLE payment_links DROP CONSTRAINT IF EXISTS payment_links_provider_check;
ALTER TABLE payment_links ADD CONSTRAINT payment_links_provider_check CHECK (provider IN ('stripe'));

ALTER TABLE bill_payments DROP CONSTRAINT IF EXISTS bill_payments_provider_check;
ALTER TABLE bill_payments ADD CONSTRAINT bill_payments_provider_check CHECK (provider IN ('stripe', 'manual'));

UPDATE branches SET card_providers = array_remove(card_providers, 'square')
 WHERE 'square' = ANY (card_providers);
ALTER TABLE branches DROP CONSTRAINT IF EXISTS branches_card_providers_valid;
ALTER TABLE branches ADD CONSTRAINT branches_card_providers_valid
    CHECK (card_providers <@ ARRAY['stripe']::TEXT[]);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE branches DROP CONSTRAINT IF EXISTS branches_card_providers_valid;
ALTER TABLE branches ADD CONSTRAINT branches_card_providers_valid
    CHECK (card_providers <@ ARRAY['stripe', 'square']::TEXT[]);

ALTER TABLE bill_payments DROP CONSTRAINT IF EXISTS bill_payments_provider_check;
ALTER TABLE bill_payments ADD CONSTRAINT bill_payments_provider_check CHECK (provider IN ('stripe', 'square', 'manual'));

ALTER TABLE payment_links DROP CONSTRAINT IF EXISTS payment_links_provider_check;
ALTER TABLE payment_links ADD CONSTRAINT payment_links_provider_check CHECK (provider IN ('stripe', 'square'));
ALTER TABLE payment_links ADD COLUMN IF NOT EXISTS provider_order_id TEXT;
CREATE INDEX IF NOT EXISTS payment_links_square_order_idx
    ON payment_links (provider_order_id) WHERE provider_order_id IS NOT NULL;

ALTER TABLE orders RENAME COLUMN provider_payment_id TO square_payment_id;
ALTER TABLE refunds RENAME COLUMN provider_refund_id TO square_refund_id;
ALTER INDEX IF EXISTS idx_payments_provider_payment RENAME TO idx_payments_square_payment;
ALTER TABLE payments RENAME COLUMN provider_payment_method_id TO square_payment_method_id;
ALTER TABLE payments RENAME COLUMN provider_payment_id TO square_payment_id;

CREATE TABLE IF NOT EXISTS square_checkout_sessions (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    square_payment_link_id TEXT NOT NULL,
    square_order_id        TEXT NOT NULL,
    amount_cents           INTEGER NOT NULL,
    currency               TEXT NOT NULL,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose StatementEnd
