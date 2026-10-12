-- +goose Up
-- +goose StatementBegin

/*
 * A gift message and occasion per order line. The storefront cart already
 * keeps a message per line (cart_items.gift_message) but it was dropped when
 * the order was placed; the POS can now send one per line too. Additive and
 * nullable: existing orders and every existing insert are unaffected.
 */
ALTER TABLE order_items ADD COLUMN IF NOT EXISTS gift_message TEXT;
ALTER TABLE order_items ADD COLUMN IF NOT EXISTS gift_reason  TEXT;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE order_items DROP COLUMN IF EXISTS gift_reason;
ALTER TABLE order_items DROP COLUMN IF EXISTS gift_message;
-- +goose StatementEnd
