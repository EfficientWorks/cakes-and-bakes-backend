-- +goose Up

-- An order already recorded how much came off (discount_cents) but never what
-- took it off. That is the question every refund conversation starts with —
-- "which code did they use?" — and the answer was nowhere in the row.
--
-- Stored as the code rather than a foreign key to coupons: the order is a
-- historical record, and it has to keep reading correctly after the coupon is
-- edited, paused or deleted. Everything else on an order line is denormalised
-- for the same reason.
ALTER TABLE orders
    ADD COLUMN discount_code TEXT;

-- "How did this campaign perform" is a report someone will want, and without
-- an index it is a full scan of every order ever placed.
CREATE INDEX idx_orders_discount_code ON orders (discount_code)
    WHERE discount_code IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS idx_orders_discount_code;
ALTER TABLE orders DROP COLUMN IF EXISTS discount_code;
