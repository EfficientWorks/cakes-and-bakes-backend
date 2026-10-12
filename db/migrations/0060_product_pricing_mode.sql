-- +goose Up
-- pricing_mode decides what an order line is charged for a product:
--   product   — the product's own base price (or the branch override), plus
--               every selected modifier delta. The original behaviour.
--   variation — the base price is ignored; the line is the sum of the chosen
--               variation option prices (plus any add-ons). Used for products
--               whose "variations" carry the full price rather than a delta.
ALTER TABLE products ADD COLUMN IF NOT EXISTS pricing_mode VARCHAR(20) NOT NULL DEFAULT 'product';
ALTER TABLE products DROP CONSTRAINT IF EXISTS products_pricing_mode_check;
ALTER TABLE products ADD CONSTRAINT products_pricing_mode_check CHECK (pricing_mode IN ('product', 'variation'));

-- +goose Down
ALTER TABLE products DROP CONSTRAINT IF EXISTS products_pricing_mode_check;
ALTER TABLE products DROP COLUMN IF EXISTS pricing_mode;
