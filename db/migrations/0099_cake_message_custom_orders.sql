-- +goose Up
-- +goose StatementBegin

/*
 * The message on a cake, customer photos, and custom orders as real orders.
 *
 * cake_message      on cart and order lines: the words piped on the cake,
 *                   kept apart from the gift-card message (gift_message).
 * photo_urls        on order lines: customer photos to print, uploaded through
 *                   POST /storefront/uploads/cake-photo. Only custom orders
 *                   carry them today; up to 3.
 * orders.source     'custom_order' for an order created from the Custom Orders
 *                   form (price to be confirmed, unpaid); NULL for every other
 *                   order, so existing rows are unchanged.
 * orders.enquiry_id the enquiry the custom order came from.
 *
 * customer_photo_uploads records every photo a customer uploaded, which is
 * how an order line proves its photo is one of ours and not any URL.
 */
ALTER TABLE cart_items  ADD COLUMN IF NOT EXISTS cake_message TEXT;
ALTER TABLE order_items ADD COLUMN IF NOT EXISTS cake_message TEXT;
ALTER TABLE order_items ADD COLUMN IF NOT EXISTS photo_urls TEXT[] NOT NULL DEFAULT '{}';

ALTER TABLE orders ADD COLUMN IF NOT EXISTS source TEXT;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS enquiry_id UUID REFERENCES enquiries (id) ON DELETE SET NULL;
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_source_check;
ALTER TABLE orders ADD CONSTRAINT orders_source_check CHECK (source IS NULL OR source IN ('custom_order'));
CREATE INDEX IF NOT EXISTS idx_orders_enquiry ON orders (enquiry_id) WHERE enquiry_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS customer_photo_uploads (
    -- The media id (32 hex characters), as in media_objects / R2.
    id           TEXT PRIMARY KEY,
    url          TEXT NOT NULL UNIQUE,
    content_type TEXT NOT NULL,
    byte_size    INTEGER NOT NULL,
    business_id  UUID REFERENCES businesses (id),
    -- For abuse triage only; never returned to a client.
    source_ip    TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS customer_photo_uploads;
DROP INDEX IF EXISTS idx_orders_enquiry;
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_source_check;
ALTER TABLE orders DROP COLUMN IF EXISTS enquiry_id;
ALTER TABLE orders DROP COLUMN IF EXISTS source;
ALTER TABLE order_items DROP COLUMN IF EXISTS photo_urls;
ALTER TABLE order_items DROP COLUMN IF EXISTS cake_message;
ALTER TABLE cart_items  DROP COLUMN IF EXISTS cake_message;

-- +goose StatementEnd
