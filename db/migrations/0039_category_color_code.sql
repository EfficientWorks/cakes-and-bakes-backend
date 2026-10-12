-- +goose Up
ALTER TABLE product_categories
    ADD COLUMN color_code TEXT;

-- +goose Down
ALTER TABLE product_categories
    DROP COLUMN IF EXISTS color_code;
