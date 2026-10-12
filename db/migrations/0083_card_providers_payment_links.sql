-- +goose Up
-- +goose StatementBegin

/*
 * Card payments through Stripe or Square, chosen per store.
 *
 * 1. branches.card_providers — which card providers a store takes payment
 *    through. Switched in admin beside the Shipday switch. The website and the
 *    POS only offer a provider that is listed here AND has its keys on the
 *    server. Defaults to Stripe, which is what every store used until now.
 *
 * 2. payment_links — a hosted payment page sent to a POS customer by email.
 *    At most one is open per order (the partial unique index): asking again
 *    re-sends it, and replacing it expires the old one at the provider first,
 *    so a customer can never pay through a stale link.
 *
 * 3. bill_payments.provider / provider_ref — which provider took a POS card
 *    payment and its id there (a Stripe PaymentIntent or a Square payment).
 *    'manual' (or NULL, for payments taken before this) is a card run on a
 *    terminal outside the system: there is nothing to refund through an API.
 *
 * Additive throughout: existing payments read as manual, existing stores as
 * Stripe-only.
 */

ALTER TABLE branches
    ADD COLUMN IF NOT EXISTS card_providers TEXT[] NOT NULL DEFAULT ARRAY['stripe']::TEXT[];
ALTER TABLE branches
    ADD CONSTRAINT branches_card_providers_valid
    CHECK (card_providers <@ ARRAY['stripe', 'square']::TEXT[]);

CREATE TABLE IF NOT EXISTS payment_links (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id            UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    -- The split share this link pays; NULL pays the order's outstanding amount.
    bill_id             UUID REFERENCES order_bills(id) ON DELETE SET NULL,
    branch_id           UUID NOT NULL,
    provider            TEXT NOT NULL CHECK (provider IN ('stripe', 'square')),
    -- Stripe: the Checkout Session id (cs_…). Square: the Payment Link id.
    provider_session_id TEXT NOT NULL,
    -- Square only: the order its Payment Link created, which is what its
    -- payment webhook names.
    provider_order_id   TEXT,
    url                 TEXT NOT NULL,
    amount_cents        INTEGER NOT NULL CHECK (amount_cents > 0),
    currency            TEXT NOT NULL DEFAULT 'USD',
    customer_email      TEXT NOT NULL,
    status              TEXT NOT NULL DEFAULT 'open'
                        CHECK (status IN ('open', 'paid', 'expired', 'cancelled')),
    -- The provider payment that paid it (pi_… or a Square payment id).
    payment_ref         TEXT,
    sent_count          INTEGER NOT NULL DEFAULT 1,
    created_by          UUID,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    paid_at             TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS payment_links_one_open_per_order
    ON payment_links (order_id) WHERE status = 'open';
CREATE INDEX IF NOT EXISTS payment_links_order_idx ON payment_links (order_id, created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS payment_links_session_idx ON payment_links (provider, provider_session_id);
CREATE INDEX IF NOT EXISTS payment_links_square_order_idx
    ON payment_links (provider_order_id) WHERE provider_order_id IS NOT NULL;

ALTER TABLE bill_payments
    ADD COLUMN IF NOT EXISTS provider        TEXT CHECK (provider IN ('stripe', 'square', 'manual')),
    ADD COLUMN IF NOT EXISTS provider_ref    TEXT,
    ADD COLUMN IF NOT EXISTS payment_link_id UUID REFERENCES payment_links(id) ON DELETE SET NULL;
-- A webhook delivered twice must not record the same provider payment twice.
CREATE UNIQUE INDEX IF NOT EXISTS bill_payments_provider_ref_idx
    ON bill_payments (provider_ref) WHERE provider_ref IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS bill_payments_provider_ref_idx;
ALTER TABLE bill_payments
    DROP COLUMN IF EXISTS payment_link_id,
    DROP COLUMN IF EXISTS provider_ref,
    DROP COLUMN IF EXISTS provider;
DROP TABLE IF EXISTS payment_links;
ALTER TABLE branches DROP CONSTRAINT IF EXISTS branches_card_providers_valid;
ALTER TABLE branches DROP COLUMN IF EXISTS card_providers;
-- +goose StatementEnd
