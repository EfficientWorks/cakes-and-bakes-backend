-- +goose Up
-- +goose StatementBegin

ALTER TABLE invoices
ALTER COLUMN franchise_id
DROP NOT NULL,
ALTER COLUMN customer_id
DROP NOT NULL,
ALTER COLUMN billed_branch_id
DROP NOT NULL,
ALTER COLUMN order_id
DROP NOT NULL,
ADD COLUMN IF NOT EXISTS customer_first_name TEXT NOT NULL DEFAULT '',
ADD COLUMN IF NOT EXISTS customer_last_name TEXT NOT NULL DEFAULT '',
ADD COLUMN IF NOT EXISTS customer_phone TEXT NOT NULL DEFAULT '',
ADD COLUMN IF NOT EXISTS customer_email TEXT NOT NULL DEFAULT '',
ADD COLUMN IF NOT EXISTS customer_address TEXT NOT NULL DEFAULT '',
ADD COLUMN IF NOT EXISTS customer_city TEXT NOT NULL DEFAULT '',
ADD COLUMN IF NOT EXISTS customer_state TEXT NOT NULL DEFAULT '',
ADD COLUMN IF NOT EXISTS is_gift BOOLEAN NOT NULL DEFAULT false,
ADD COLUMN IF NOT EXISTS receiver_first_name TEXT NOT NULL DEFAULT '',
ADD COLUMN IF NOT EXISTS receiver_last_name TEXT NOT NULL DEFAULT '',
ADD COLUMN IF NOT EXISTS receiver_phone TEXT NOT NULL DEFAULT '',
ADD COLUMN IF NOT EXISTS receiver_email TEXT NOT NULL DEFAULT '',
ADD COLUMN IF NOT EXISTS receiver_address TEXT NOT NULL DEFAULT '',
ADD COLUMN IF NOT EXISTS receiver_city TEXT NOT NULL DEFAULT '',
ADD COLUMN IF NOT EXISTS receiver_state TEXT NOT NULL DEFAULT '',
DROP CONSTRAINT IF EXISTS invoices_billed_party;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE invoices
ALTER COLUMN franchise_id
SET NOT NULL,
ALTER COLUMN customer_id
SET NOT NULL,
ALTER COLUMN billed_branch_id
SET NOT NULL,
ALTER COLUMN order_id
SET NOT NULL,
DROP COLUMN IF EXISTS customer_first_name,
DROP COLUMN IF EXISTS customer_last_name,
DROP COLUMN IF EXISTS customer_phone,
DROP COLUMN IF EXISTS customer_email,
DROP COLUMN IF EXISTS customer_address,
DROP COLUMN IF EXISTS customer_city,
DROP COLUMN IF EXISTS customer_state,
DROP COLUMN IF EXISTS is_gift,
DROP COLUMN IF EXISTS receiver_first_name,
DROP COLUMN IF EXISTS receiver_last_name,
DROP COLUMN IF EXISTS receiver_phone,
DROP COLUMN IF EXISTS receiver_email,
DROP COLUMN IF EXISTS receiver_address,
DROP COLUMN IF EXISTS receiver_city,
DROP COLUMN IF EXISTS receiver_state,
ADD CONSTRAINT invoices_billed_party CHECK (
    (
        kind = 'customer'
        AND customer_id IS NOT NULL
        AND billed_branch_id IS NULL
    )
    OR (
        kind = 'store'
        AND billed_branch_id IS NOT NULL
        AND customer_id IS NULL
    )
);

-- +goose StatementEnd