-- +goose Up

-- The old model allowed records to belong only to a franchise. Preserve those
-- records by routing them to one explicit branch before removing the parent.
INSERT INTO branches (
    id, franchise_id, name, code, type, ownership_type, street, city, state, postcode, country,
    timezone, opening_hours, fulfilment_types, delivery_zone, status
)
SELECT '00000000-0000-0000-0000-000000000070',
       (SELECT id FROM franchises ORDER BY created_at LIMIT 1),
       'Legacy default branch', 'LEGACY-DEFAULT', 'retail', 'corporate store',
       '1 Main Street', 'Edison', 'NJ', '08817', 'US', 'America/New_York',
       '[]'::jsonb, ARRAY['pickup'], '{"type":"zip_list","zips":[]}'::jsonb, 'active'
WHERE NOT EXISTS (SELECT 1 FROM branches WHERE id = '00000000-0000-0000-0000-000000000070');

-- Preserve franchise-scoped staff access where a concrete branch cannot be
-- inferred. Existing branch-scoped assignments remain unchanged.
UPDATE staff_role_assignments
SET scope_type = 'branch',
    branch_id = '00000000-0000-0000-0000-000000000070'
WHERE scope_type = 'franchise' AND branch_id IS NULL;

-- This is a controlled historical backfill. The normal append-only trigger
-- remains enabled before and after the migration.
ALTER TABLE audit_log DISABLE TRIGGER trg_audit_log_no_update;

UPDATE audit_log
SET branch_id = '00000000-0000-0000-0000-000000000070'
WHERE branch_id IS NULL AND franchise_id IS NOT NULL;

ALTER TABLE audit_log ENABLE TRIGGER trg_audit_log_no_update;

UPDATE domain_events
SET branch_id = '00000000-0000-0000-0000-000000000070'
WHERE branch_id IS NULL AND franchise_id IS NOT NULL;

UPDATE enquiries
SET branch_id = '00000000-0000-0000-0000-000000000070'
WHERE branch_id IS NULL AND franchise_id IS NOT NULL;

-- Remove indexes and foreign keys before dropping the parent columns.
DROP INDEX IF EXISTS idx_audit_log_franchise;
DROP INDEX IF EXISTS idx_orders_franchise;
DROP INDEX IF EXISTS invoices_franchise_idx;
DROP INDEX IF EXISTS idx_branch_requests_franchise;

ALTER TABLE audit_log DROP COLUMN IF EXISTS franchise_id;
ALTER TABLE domain_events DROP COLUMN IF EXISTS franchise_id;
ALTER TABLE enquiries DROP COLUMN IF EXISTS franchise_id;
ALTER TABLE orders DROP COLUMN IF EXISTS franchise_id;
ALTER TABLE payments DROP COLUMN IF EXISTS franchise_id;
ALTER TABLE invoices DROP COLUMN IF EXISTS franchise_id;
ALTER TABLE branch_requests DROP COLUMN IF EXISTS franchise_id;
ALTER TABLE pos_printers DROP COLUMN IF EXISTS franchise_id;
ALTER TABLE pos_cash_days DROP COLUMN IF EXISTS franchise_id;
ALTER TABLE pos_cash_movements DROP COLUMN IF EXISTS franchise_id;
ALTER TABLE print_receipt_templates DROP COLUMN IF EXISTS franchise_id;
ALTER TABLE print_kot_templates DROP COLUMN IF EXISTS franchise_id;
ALTER TABLE print_cancel_templates DROP COLUMN IF EXISTS franchise_id;
ALTER TABLE branches DROP COLUMN IF EXISTS franchise_id;

ALTER TABLE staff_role_assignments
    DROP CONSTRAINT IF EXISTS chk_scope_consistency,
    DROP COLUMN IF EXISTS franchise_id;

-- All application columns have been removed above. CASCADE clears any
-- historical foreign-key constraint objects left by earlier migrations.
DROP TABLE IF EXISTS franchises CASCADE;

-- +goose Down
-- Franchise relationships cannot be reconstructed after this migration.