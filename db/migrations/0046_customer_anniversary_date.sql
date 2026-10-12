-- +goose Up
-- +goose StatementBegin
ALTER TABLE customers
    ALTER COLUMN email DROP NOT NULL,
    ADD COLUMN IF NOT EXISTS date_of_birth   DATE,
    ADD COLUMN IF NOT EXISTS anniversary_date DATE;

ALTER TABLE customer_addresses
    ADD COLUMN IF NOT EXISTS address TEXT;

ALTER TABLE customer_addresses ALTER COLUMN street DROP NOT NULL;
ALTER TABLE customer_addresses ALTER COLUMN street SET DEFAULT '';
ALTER TABLE customer_addresses ALTER COLUMN city DROP NOT NULL;
ALTER TABLE customer_addresses ALTER COLUMN city SET DEFAULT '';
ALTER TABLE customer_addresses ALTER COLUMN state DROP NOT NULL;
ALTER TABLE customer_addresses ALTER COLUMN state SET DEFAULT '';
ALTER TABLE customer_addresses ALTER COLUMN postcode DROP NOT NULL;
ALTER TABLE customer_addresses ALTER COLUMN postcode SET DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE customers
    DROP COLUMN IF EXISTS anniversary_date,
    DROP COLUMN IF EXISTS date_of_birth;
-- +goose StatementEnd

