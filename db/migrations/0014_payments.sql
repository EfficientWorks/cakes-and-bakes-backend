-- +goose Up

-- One row per payment attempt against an order. amount_cents mirrors
-- orders.total_cents at capture time; card data never touches this table
-- (PAY-09) — only Stripe's own tokenised references are stored.
CREATE TABLE payments (
    id                        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id                  UUID NOT NULL REFERENCES orders(id),
    franchise_id              UUID NOT NULL,
    branch_id                 UUID NOT NULL,
    method                    TEXT NOT NULL DEFAULT 'card' CHECK (method IN ('card','cash','apple_pay','google_pay','terminal','manual_counter')),
    amount_cents              INTEGER NOT NULL,
    currency                  TEXT NOT NULL DEFAULT 'USD',
    stripe_payment_intent_id  TEXT,
    stripe_charge_id          TEXT,
    stripe_payment_method_id  TEXT,
    status                    TEXT NOT NULL DEFAULT 'pending' CHECK (status IN
        ('pending','captured','failed','refunded','partial_refund')),
    failure_reason            TEXT,
    captured_at               TIMESTAMPTZ,
    created_at                TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_payments_order ON payments (order_id);
CREATE UNIQUE INDEX idx_payments_stripe_intent ON payments (stripe_payment_intent_id) WHERE stripe_payment_intent_id IS NOT NULL;

CREATE TABLE refunds (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    payment_id          UUID NOT NULL REFERENCES payments(id),
    order_id            UUID NOT NULL REFERENCES orders(id),
    amount_cents        INTEGER NOT NULL,
    reason              TEXT NOT NULL,
    stripe_refund_id    TEXT,
    status              TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','succeeded','failed')),
    issued_by_user_id   UUID,
    approved_by_user_id UUID,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_refunds_order ON refunds (order_id);
CREATE INDEX idx_refunds_payment ON refunds (payment_id);

-- +goose Down
DROP TABLE IF EXISTS refunds;
DROP TABLE IF EXISTS payments;
