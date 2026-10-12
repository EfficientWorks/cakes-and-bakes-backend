-- +goose Up
-- +goose StatementBegin

-- Takeaway joins pickup and delivery.
--
-- The counter sells it: someone orders at the till and walks out with the box.
-- That is neither of the two the website needs — pickup means an order placed
-- ahead and collected later, delivery means it is taken to them — and filing
-- it as pickup made every "collected later" figure in the reports wrong by
-- however much the counter sold that day.
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_fulfilment_type_check;
ALTER TABLE orders ADD CONSTRAINT orders_fulfilment_type_check
  CHECK (fulfilment_type IN ('pickup', 'delivery', 'takeaway'));

CREATE INDEX IF NOT EXISTS orders_fulfilment_type_idx
  ON orders (fulfilment_type) WHERE deleted_at IS NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
UPDATE orders SET fulfilment_type = 'pickup' WHERE fulfilment_type = 'takeaway';
DROP INDEX IF EXISTS orders_fulfilment_type_idx;
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_fulfilment_type_check;
ALTER TABLE orders ADD CONSTRAINT orders_fulfilment_type_check
  CHECK (fulfilment_type IN ('pickup', 'delivery'));
-- +goose StatementEnd
