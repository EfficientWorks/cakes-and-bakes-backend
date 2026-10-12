-- +goose Up
-- +goose StatementBegin

/*
 * Discount and tax per returned item.
 *
 * A return used to price its discount and tax once, on the summed value of
 * everything handed back, from the order-level rates; each returned item kept
 * only its quantity and gross amount. An item with its own discount was then
 * refunded at full price, and nothing said what any one item's refund was.
 *
 * Now each item is priced on its own (its own discount prorated by the
 * quantity returned, plus its share of the order discount, then tax on what
 * is left) and stored here. order_returns and orders keep the totals, which
 * are the sums of these.
 *
 *   refund_cents = amount_cents - discount_cents + tax_cents
 *
 * Additive with defaults: returns recorded before this read as gross only.
 */
ALTER TABLE order_return_items ADD COLUMN IF NOT EXISTS discount_pct   NUMERIC(5,2);
ALTER TABLE order_return_items ADD COLUMN IF NOT EXISTS discount_cents INTEGER NOT NULL DEFAULT 0;
ALTER TABLE order_return_items ADD COLUMN IF NOT EXISTS tax_pct        NUMERIC(6,3);
ALTER TABLE order_return_items ADD COLUMN IF NOT EXISTS tax_cents      INTEGER NOT NULL DEFAULT 0;
ALTER TABLE order_return_items ADD COLUMN IF NOT EXISTS refund_cents   INTEGER;

-- Returns recorded before: the refund was the gross for each item.
UPDATE order_return_items SET refund_cents = amount_cents WHERE refund_cents IS NULL;

-- +goose StatementEnd

-- +goose Down
ALTER TABLE order_return_items DROP COLUMN IF EXISTS refund_cents;
ALTER TABLE order_return_items DROP COLUMN IF EXISTS tax_cents;
ALTER TABLE order_return_items DROP COLUMN IF EXISTS tax_pct;
ALTER TABLE order_return_items DROP COLUMN IF EXISTS discount_cents;
ALTER TABLE order_return_items DROP COLUMN IF EXISTS discount_pct;
