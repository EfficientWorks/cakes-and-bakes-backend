-- +goose Up
-- +goose StatementBegin

-- Allow product_id to be NULL for manual/custom counter order items (e.g. custom candles, misc items)
ALTER TABLE order_items ALTER COLUMN product_id DROP NOT NULL;

-- Allow production_status to include 'cancelled', 'cancel', 'returned' for POS item status updates
ALTER TABLE order_items DROP CONSTRAINT IF EXISTS order_items_production_status_check;
ALTER TABLE order_items ADD CONSTRAINT order_items_production_status_check
  CHECK (production_status IN ('pending', 'in_progress', 'done', 'cancelled', 'cancel', 'returned'));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE order_items DROP CONSTRAINT IF EXISTS order_items_production_status_check;
ALTER TABLE order_items ADD CONSTRAINT order_items_production_status_check
  CHECK (production_status IN ('pending', 'in_progress', 'done', 'cancelled'));

ALTER TABLE order_items ALTER COLUMN product_id SET NOT NULL;
-- +goose StatementEnd
