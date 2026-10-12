-- +goose Up

-- A payment row is written before its order exists, so it cannot demand one.
--
-- Checkout captures the money first and only creates the order if that
-- succeeds — deliberately, so a failed payment never leaves a phantom order
-- behind (see paymentsapi.Processor). The payment row is therefore inserted
-- with no order_id, and LinkOrder attaches one immediately afterwards.
--
-- These three columns were NOT NULL, which made that insert fail every single
-- time. The error was discarded as best-effort, so nothing surfaced: the
-- payments table simply stayed empty. Every capture, decline and 3-D Secure
-- challenge since launch went unrecorded.
--
-- Nullable until linked. A payment that never reaches an order — declined, or
-- abandoned mid-authentication — keeps them null for good, which is exactly
-- the audit trail that was missing.
ALTER TABLE payments ALTER COLUMN order_id DROP NOT NULL;
ALTER TABLE payments ALTER COLUMN franchise_id DROP NOT NULL;
ALTER TABLE payments ALTER COLUMN branch_id DROP NOT NULL;

-- +goose Down

-- Rows recorded before their order was linked cannot satisfy NOT NULL, so
-- they are removed rather than blocking the rollback.
DELETE FROM payments WHERE order_id IS NULL OR franchise_id IS NULL OR branch_id IS NULL;
ALTER TABLE payments ALTER COLUMN branch_id SET NOT NULL;
ALTER TABLE payments ALTER COLUMN franchise_id SET NOT NULL;
ALTER TABLE payments ALTER COLUMN order_id SET NOT NULL;
