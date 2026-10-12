-- +goose Up
-- When the KDS recalled a handed-over order back to the ready rail; cleared
-- when it is handed over again. The KDS shows it as isRecalled.
ALTER TABLE orders ADD COLUMN kds_recalled_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE orders DROP COLUMN IF EXISTS kds_recalled_at;
