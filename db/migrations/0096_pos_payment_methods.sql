-- +goose Up
-- +goose StatementBegin

/*
 * Payment methods per sales channel.
 *
 * Until now one pair of switches served both the website and the till:
 * accepts_cash_on_delivery and card_providers. A store can now take cash or
 * Stripe on one channel and not the other, so the till gets its own pair:
 *
 *   accepts_cash_on_delivery, card_providers   the website (unchanged names,
 *                                              so the storefront keeps reading
 *                                              them as it does today)
 *   pos_accepts_cash, pos_card_providers       the POS till
 *
 * The POS pair starts as a copy of the shared one, so no store's till changes
 * what it offers when this runs.
 */
ALTER TABLE branches
    ADD COLUMN pos_accepts_cash   BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN pos_card_providers TEXT[]  NOT NULL DEFAULT ARRAY['stripe']::TEXT[];

UPDATE branches SET pos_accepts_cash = accepts_cash_on_delivery, pos_card_providers = card_providers;

ALTER TABLE branches ADD CONSTRAINT branches_pos_card_providers_valid
    CHECK (pos_card_providers <@ ARRAY['stripe']::TEXT[]);

COMMENT ON COLUMN branches.accepts_cash_on_delivery IS 'Website: customer may pay cash on pickup/delivery.';
COMMENT ON COLUMN branches.card_providers IS 'Website: online card providers offered at checkout.';
COMMENT ON COLUMN branches.pos_accepts_cash IS 'POS: till may take cash.';
COMMENT ON COLUMN branches.pos_card_providers IS 'POS: online card providers for payment links.';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE branches DROP CONSTRAINT IF EXISTS branches_pos_card_providers_valid;
ALTER TABLE branches DROP COLUMN IF EXISTS pos_card_providers;
ALTER TABLE branches DROP COLUMN IF EXISTS pos_accepts_cash;

-- +goose StatementEnd
