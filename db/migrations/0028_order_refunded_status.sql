-- +goose Up
-- +goose StatementBegin

-- A fully refunded order is a state of its own.
--
-- Refunding only ever moved `payment_status`, so an order refunded in full
-- still read as "confirmed" everywhere an order's status is shown — on the
-- kitchen screen, in the admin list, to the customer. Cancelled is not the
-- right word for it either: the order was placed, accepted, and possibly made
-- and delivered. What happened is that the money went back.
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_status_check;
ALTER TABLE orders ADD CONSTRAINT orders_status_check CHECK (status IN (
  'placed', 'confirmed', 'scheduled', 'in_production', 'ready',
  'out_for_delivery', 'fulfilled', 'cancelled', 'refunded'
));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
UPDATE orders SET status = 'cancelled' WHERE status = 'refunded';
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_status_check;
ALTER TABLE orders ADD CONSTRAINT orders_status_check CHECK (status IN (
  'placed', 'confirmed', 'scheduled', 'in_production', 'ready',
  'out_for_delivery', 'fulfilled', 'cancelled'
));
-- +goose StatementEnd
