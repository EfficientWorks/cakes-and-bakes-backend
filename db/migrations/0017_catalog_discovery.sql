-- +goose Up
-- Storefront discovery: search, sort and filter support for the Shop Now grid.

-- Trigram matching gives the search box partial-word hits ("bloss" -> "Berry
-- Blossom"), which a plain tsvector index does not.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- "Most gifted" is a real ranking, not a proxy for best-seller. It is fed from
-- order volume by a rollup outside the catalogue service (admin owns the order
-- read model), so this column is written by that feed and stays 0 until it
-- runs. Sorting falls back to is_best_seller and sort_order for ties, which
-- keeps ordering stable and sensible on a cold database.
ALTER TABLE products ADD COLUMN popularity_score INTEGER NOT NULL DEFAULT 0;

CREATE INDEX idx_products_popularity ON products (popularity_score DESC, sort_order)
    WHERE deleted_at IS NULL AND is_active = true;

CREATE INDEX idx_products_base_price ON products (base_price_cents)
    WHERE deleted_at IS NULL AND is_active = true;

-- GIN index so the dietary chips are index-backed rather than a sequential
-- scan. occasion_tags already has one from 0009_catalog.sql.
CREATE INDEX idx_products_dietary_flags ON products USING GIN (dietary_flags);

CREATE INDEX idx_products_name_trgm ON products USING GIN (name gin_trgm_ops);
CREATE INDEX idx_products_description_trgm ON products USING GIN (description gin_trgm_ops);

-- +goose Down
DROP INDEX IF EXISTS idx_products_description_trgm;
DROP INDEX IF EXISTS idx_products_name_trgm;
DROP INDEX IF EXISTS idx_products_dietary_flags;
DROP INDEX IF EXISTS idx_products_base_price;
DROP INDEX IF EXISTS idx_products_popularity;
ALTER TABLE products DROP COLUMN IF EXISTS popularity_score;
