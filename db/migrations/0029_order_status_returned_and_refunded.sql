-- +goose Up
-- +goose StatementBegin

-- Both branches added an order status, and the second one erased the first.
--
-- 0027 (point of sale) added 'returned' — goods handed back over a counter.
-- 0028 (refunds) added 'refunded' — the order stood and the money went back.
-- Each rewrote the whole CHECK from the list it knew about, so applying them in
-- order left only the later one, and POS returns would be refused by the
-- database with a constraint violation.
--
-- They are genuinely different states and both are wanted:
--
--   returned — the customer gave the goods back
--   refunded — the customer kept them, or never got them, and was repaid
--
-- Written as one list so the next status to be added has an obvious place to go
-- rather than a third copy of this problem.
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_status_check;
ALTER TABLE orders ADD CONSTRAINT orders_status_check CHECK (status IN (
  'placed', 'confirmed', 'scheduled', 'in_production', 'ready',
  'out_for_delivery', 'fulfilled', 'cancelled', 'returned', 'refunded'
));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
UPDATE orders SET status = 'cancelled' WHERE status IN ('returned', 'refunded');
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_status_check;
ALTER TABLE orders ADD CONSTRAINT orders_status_check CHECK (status IN (
  'placed', 'confirmed', 'scheduled', 'in_production', 'ready',
  'out_for_delivery', 'fulfilled', 'cancelled'
));
-- +goose StatementEnd
