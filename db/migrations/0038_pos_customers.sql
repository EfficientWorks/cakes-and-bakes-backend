-- +goose Up
ALTER TABLE customers
    ADD COLUMN IF NOT EXISTS branch_id UUID REFERENCES branches(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS customer_type TEXT NOT NULL DEFAULT 'pos';

CREATE INDEX IF NOT EXISTS idx_customers_branch_type ON customers (branch_id, customer_type) WHERE deleted_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_customers_branch_type;
ALTER TABLE customers
    DROP COLUMN IF EXISTS customer_type,
    DROP COLUMN IF EXISTS branch_id;
