-- +goose Up
-- +goose StatementBegin

/*
 * Option-set combinations: one row per Size × Sponge × Cream (or any set of
 * variation groups) a product is sold in.
 *
 *   option_ids    one modifier option from each variation group the product
 *                 carries, stored sorted so the same choice always has the
 *                 same key. No FK: an option that is later deleted simply
 *                 stops matching, and the row is ignored until edited.
 *   price_cents   NULL = automatic (the sum of the chosen options' prices,
 *                 store prices included); a number = the price staff typed.
 *   is_active     the console's On/Off. Off rows are never offered or sold.
 *
 * A product with no rows here behaves exactly as before.
 */
CREATE TABLE IF NOT EXISTS product_combinations (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id  UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    option_ids  UUID[] NOT NULL CHECK (cardinality(option_ids) > 0),
    price_cents BIGINT CHECK (price_cents IS NULL OR price_cents >= 0),
    is_active   BOOLEAN NOT NULL DEFAULT true,
    sort_order  INTEGER NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS product_combinations_product_options_key
    ON product_combinations (product_id, option_ids);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS product_combinations;

-- +goose StatementEnd
