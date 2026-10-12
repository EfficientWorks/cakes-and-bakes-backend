-- +goose Up

-- Delivery details are optional because pickup orders and recipients without
-- a delivery address should still be saved normally.
ALTER TABLE storefront_recipients
    ADD COLUMN IF NOT EXISTS delivery_instructions TEXT,
    ADD COLUMN IF NOT EXISTS delivery_postcode TEXT,
    ADD COLUMN IF NOT EXISTS delivery_state TEXT,
    ADD COLUMN IF NOT EXISTS delivery_city TEXT,
    ADD COLUMN IF NOT EXISTS delivery_street TEXT;

-- +goose Down
ALTER TABLE storefront_recipients
    DROP COLUMN IF EXISTS delivery_street,
    DROP COLUMN IF EXISTS delivery_city,
    DROP COLUMN IF EXISTS delivery_state,
    DROP COLUMN IF EXISTS delivery_postcode,
    DROP COLUMN IF EXISTS delivery_instructions;