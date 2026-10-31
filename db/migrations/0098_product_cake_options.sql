-- +goose Up
-- +goose StatementBegin

/*
 * Product code, colour and sponge filters, and the message piped on a cake.
 *
 *   code                     the shop's own code ("WPC-040"), stored trimmed
 *                            and upper-cased; unique within a business among
 *                            live products when set
 *   colours, sponges         filter values; the allowed lists are enforced by
 *                            the service (catalogapi.ProductColours/Sponges)
 *   cake_message_mode        off | optional | required — existing products off
 *   cake_message_max_length  characters, 1–200, default 40
 *   cake_message_presets     the chips the product page offers
 *
 * Every column has a default, so existing rows need no backfill and the
 * POS/website keep reading them unchanged.
 */
ALTER TABLE products ADD COLUMN IF NOT EXISTS code TEXT;
ALTER TABLE products ADD COLUMN IF NOT EXISTS colours TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE products ADD COLUMN IF NOT EXISTS sponges TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE products ADD COLUMN IF NOT EXISTS cake_message_mode TEXT NOT NULL DEFAULT 'off';
ALTER TABLE products ADD COLUMN IF NOT EXISTS cake_message_max_length INTEGER NOT NULL DEFAULT 40;
ALTER TABLE products ADD COLUMN IF NOT EXISTS cake_message_presets TEXT[] NOT NULL
    DEFAULT ARRAY['Happy Birthday', 'Happy Anniversary', 'Congratulations', 'Happy Wedding Anniversary'];

ALTER TABLE products DROP CONSTRAINT IF EXISTS products_code_check;
ALTER TABLE products ADD CONSTRAINT products_code_check
    CHECK (code IS NULL OR (code <> '' AND char_length(code) <= 32 AND code = upper(btrim(code))));
ALTER TABLE products DROP CONSTRAINT IF EXISTS products_cake_message_mode_check;
ALTER TABLE products ADD CONSTRAINT products_cake_message_mode_check
    CHECK (cake_message_mode IN ('off', 'optional', 'required'));
ALTER TABLE products DROP CONSTRAINT IF EXISTS products_cake_message_max_length_check;
ALTER TABLE products ADD CONSTRAINT products_cake_message_max_length_check
    CHECK (cake_message_max_length BETWEEN 1 AND 200);

-- Deleted products give their code back.
CREATE UNIQUE INDEX IF NOT EXISTS products_business_code_key
    ON products (business_id, code) NULLS NOT DISTINCT
    WHERE code IS NOT NULL AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_products_colours ON products USING GIN (colours);
CREATE INDEX IF NOT EXISTS idx_products_sponges ON products USING GIN (sponges);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_products_sponges;
DROP INDEX IF EXISTS idx_products_colours;
DROP INDEX IF EXISTS products_business_code_key;
ALTER TABLE products DROP CONSTRAINT IF EXISTS products_cake_message_max_length_check;
ALTER TABLE products DROP CONSTRAINT IF EXISTS products_cake_message_mode_check;
ALTER TABLE products DROP CONSTRAINT IF EXISTS products_code_check;
ALTER TABLE products DROP COLUMN IF EXISTS cake_message_presets;
ALTER TABLE products DROP COLUMN IF EXISTS cake_message_max_length;
ALTER TABLE products DROP COLUMN IF EXISTS cake_message_mode;
ALTER TABLE products DROP COLUMN IF EXISTS sponges;
ALTER TABLE products DROP COLUMN IF EXISTS colours;
ALTER TABLE products DROP COLUMN IF EXISTS code;

-- +goose StatementEnd
