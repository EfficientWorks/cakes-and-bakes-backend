-- +goose Up
-- +goose StatementBegin

-- The fields the till keeps against a line.
--
-- The POS screen owns the state of an order while it is being worked on: it
-- decides what has gone to the kitchen, what has been cancelled, and how much
-- of each line stands. It then sends the whole picture and the server records
-- it. So these are stored as given rather than derived here.
ALTER TABLE order_items
  ADD COLUMN IF NOT EXISTS send_kot_time TIMESTAMPTZ;

-- 'cancelled' joins the line states.
--
-- A cancelled line is kept, not deleted: the kitchen was handed a docket with
-- it on, so the row has to stay to explain the difference between what was
-- printed and what is being charged. It simply stops counting towards the
-- total.
ALTER TABLE order_items DROP CONSTRAINT IF EXISTS order_items_production_status_check;
ALTER TABLE order_items ADD CONSTRAINT order_items_production_status_check
  CHECK (production_status IN ('pending', 'in_progress', 'done', 'cancelled'));

CREATE INDEX IF NOT EXISTS order_items_order_batch_idx
  ON order_items (order_id, batch_no);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
UPDATE order_items SET production_status = 'pending' WHERE production_status = 'cancelled';
ALTER TABLE order_items DROP CONSTRAINT IF EXISTS order_items_production_status_check;
ALTER TABLE order_items ADD CONSTRAINT order_items_production_status_check
  CHECK (production_status IN ('pending', 'in_progress', 'done'));
DROP INDEX IF EXISTS order_items_order_batch_idx;
ALTER TABLE order_items DROP COLUMN IF EXISTS send_kot_time;
-- +goose StatementEnd
