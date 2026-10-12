-- +goose Up
-- +goose StatementBegin
ALTER TABLE orders ADD COLUMN IF NOT EXISTS bill_no INTEGER;

CREATE TABLE IF NOT EXISTS branch_bill_counters (
    branch_id    UUID PRIMARY KEY REFERENCES branches(id),
    last_bill_no INTEGER NOT NULL DEFAULT 0,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Backfill existing orders per branch in creation order.
WITH numbered AS (
    SELECT id, ROW_NUMBER() OVER (PARTITION BY branch_id ORDER BY created_at, id) AS rn
    FROM orders
)
UPDATE orders o SET bill_no = n.rn FROM numbered n WHERE o.id = n.id AND o.bill_no IS NULL;

INSERT INTO branch_bill_counters (branch_id, last_bill_no)
SELECT branch_id, COALESCE(MAX(bill_no), 0) FROM orders GROUP BY branch_id
ON CONFLICT (branch_id) DO UPDATE SET last_bill_no = EXCLUDED.last_bill_no;

CREATE UNIQUE INDEX IF NOT EXISTS orders_branch_bill_no_key ON orders (branch_id, bill_no);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS orders_branch_bill_no_key;
DROP TABLE IF EXISTS branch_bill_counters;
ALTER TABLE orders DROP COLUMN IF EXISTS bill_no;
-- +goose StatementEnd