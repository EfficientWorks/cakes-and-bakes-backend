-- +goose Up
ALTER TABLE products ADD COLUMN IF NOT EXISTS show_on_pos BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE products ADD COLUMN IF NOT EXISTS show_on_website BOOLEAN NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE products DROP COLUMN IF EXISTS show_on_pos;
ALTER TABLE products DROP COLUMN IF EXISTS show_on_website;
