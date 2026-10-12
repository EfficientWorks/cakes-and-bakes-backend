-- +goose Up
-- +goose StatementBegin

-- 1. Allow email to be NULL or empty on customers table for walk-in / POS customers
ALTER TABLE customers ALTER COLUMN email DROP NOT NULL;
ALTER TABLE customers DROP CONSTRAINT IF EXISTS customers_email_key;
DROP INDEX IF EXISTS idx_customers_email;
CREATE UNIQUE INDEX IF NOT EXISTS idx_customers_email ON customers (lower(email))
    WHERE deleted_at IS NULL AND email IS NOT NULL AND email <> '';

-- 2. Add address field to customer_addresses table
ALTER TABLE customer_addresses ADD COLUMN IF NOT EXISTS address TEXT;

-- 3. Relax not-null constraints on customer_addresses components so city & postcode inserts succeed
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
ALTER TABLE customer_addresses DROP COLUMN IF EXISTS address;
-- +goose StatementEnd
