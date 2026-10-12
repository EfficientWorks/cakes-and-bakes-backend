-- +goose Up

-- A former franchise assignment means access to every branch below it. Expand
-- that assignment into branch assignments before removing the franchise scope.
INSERT INTO staff_role_assignments (
    staff_user_id, role, scope_type, branch_id,
    max_refund_cents, max_discount_pct, order_void_window_mins
)
SELECT sra.staff_user_id, sra.role, 'branch', b.id,
       sra.max_refund_cents, sra.max_discount_pct, sra.order_void_window_mins
FROM staff_role_assignments sra
JOIN branches b ON b.franchise_id = sra.franchise_id
WHERE sra.scope_type = 'franchise'
  AND NOT EXISTS (
      SELECT 1
      FROM staff_role_assignments existing
      WHERE existing.staff_user_id = sra.staff_user_id
        AND existing.role = sra.role
        AND existing.scope_type = 'branch'
        AND existing.branch_id = b.id
  );

DELETE FROM staff_role_assignments WHERE scope_type = 'franchise';

-- +goose Down
-- The expansion is intentionally not reversed: branch assignments may have
-- been edited after migration and cannot be losslessly recombined.