-- +goose Up

-- The rate the order's tax was computed at. Before this, only tax_cents was
-- stored and the API derived a percentage from tax / (subtotal - discount),
-- which drifted (5% became 5.17%) whenever a line discount changed the
-- subtotal without the tax being recomputed.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS tax_pct NUMERIC(6,3);

-- +goose Down
ALTER TABLE orders DROP COLUMN IF EXISTS tax_pct;
