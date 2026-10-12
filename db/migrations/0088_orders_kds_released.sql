-- +goose Up
-- When the till started a later-dated order early (POS "Start Process"):
-- the KDS shows it today although it is due on another day.
ALTER TABLE orders ADD COLUMN kds_released_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE orders DROP COLUMN IF EXISTS kds_released_at;
