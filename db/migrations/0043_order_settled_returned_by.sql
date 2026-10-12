-- +goose Up
-- +goose StatementBegin

-- Who settled the order (staff ID from token when payment_status → captured)
ALTER TABLE orders ADD COLUMN IF NOT EXISTS settled_by TEXT;

-- Who marked the order fulfilled (staff ID from token when status → fulfilled)
ALTER TABLE orders ADD COLUMN IF NOT EXISTS returned_by TEXT;

-- How long the order took from creation to settlement, in seconds.
-- Populated when the order is fully paid (payment_status → captured).
ALTER TABLE orders ADD COLUMN IF NOT EXISTS total_duration_secs INTEGER;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE orders DROP COLUMN IF EXISTS settled_by;
ALTER TABLE orders DROP COLUMN IF EXISTS returned_by;
ALTER TABLE orders DROP COLUMN IF EXISTS total_duration_secs;
-- +goose StatementEnd
