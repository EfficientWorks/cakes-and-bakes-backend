-- +goose Up

-- When an order was opened and when it was closed, as their own columns.
--
-- The API used to report startTime/endTime from created_at/updated_at, so
-- "end" moved every time anyone touched the order — a note added a week
-- later became the end time. start_time is stamped once at creation (the
-- event time when the till sends one); end_time is stamped once, the first
-- time the order reaches a closing state: settled, fulfilled, cancelled or
-- returned. Neither moves after that.
ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS start_time TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS end_time   TIMESTAMPTZ;

-- Backfill from what was known: creation for the start; for the end, the
-- duration the settle path already recorded, else the last update on orders
-- that had already closed.
UPDATE orders SET start_time = created_at WHERE start_time IS NULL;
UPDATE orders
   SET end_time = CASE
                    WHEN total_duration_secs IS NOT NULL THEN created_at + make_interval(secs => total_duration_secs)
                    WHEN status IN ('fulfilled', 'settled', 'cancelled', 'returned')
                      OR lower(payment_status) IN ('captured', 'paid') THEN updated_at
                    ELSE NULL
                  END
 WHERE end_time IS NULL;

ALTER TABLE orders ALTER COLUMN start_time SET DEFAULT NOW();

-- +goose Down
ALTER TABLE orders
    DROP COLUMN IF EXISTS end_time,
    DROP COLUMN IF EXISTS start_time;
