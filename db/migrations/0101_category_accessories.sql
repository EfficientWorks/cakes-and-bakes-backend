-- +goose Up
-- +goose StatementBegin

/*
 * A category can be marked as accessories (candles, toppers, balloons). The
 * website offers its products after Add to Basket and in the basket. Existing
 * categories are not accessories.
 */
ALTER TABLE product_categories ADD COLUMN IF NOT EXISTS is_accessories BOOLEAN NOT NULL DEFAULT false;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE product_categories DROP COLUMN IF EXISTS is_accessories;

-- +goose StatementEnd
