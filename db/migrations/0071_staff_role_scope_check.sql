-- +goose Up

-- 0070 dropped chk_scope_consistency together with staff_role_assignments.franchise_id
-- and did not replace it, so the table would accept a 'franchise' scope row
-- (or a branch scope with no branch) that grants access to nothing. The
-- application validates this too; the database is the backstop (CLAUDE.md
-- invariant 6).
--
-- NOT VALID: enforced for every new or updated row, without scanning existing
-- ones — migrations run at server start-up, and a legacy row that happened to
-- violate this must not stop the API from booting. Run
--   ALTER TABLE staff_role_assignments VALIDATE CONSTRAINT chk_scope_consistency;
-- once the query below returns no rows:
--   SELECT * FROM staff_role_assignments
--    WHERE NOT ((scope_type = 'brand'  AND branch_id IS NULL) OR
--               (scope_type = 'branch' AND branch_id IS NOT NULL));
ALTER TABLE staff_role_assignments DROP CONSTRAINT IF EXISTS chk_scope_consistency;
ALTER TABLE staff_role_assignments
    ADD CONSTRAINT chk_scope_consistency CHECK (
        (scope_type = 'brand'  AND branch_id IS NULL) OR
        (scope_type = 'branch' AND branch_id IS NOT NULL)
    ) NOT VALID;

-- +goose Down
ALTER TABLE staff_role_assignments DROP CONSTRAINT IF EXISTS chk_scope_consistency;
