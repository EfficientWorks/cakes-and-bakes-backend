-- +goose Up

-- Staff accounts are separate from customer accounts and can never be
-- escalated into a customer role or vice versa (RBAC-11).
CREATE TABLE staff_users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email           TEXT UNIQUE NOT NULL,
    password_hash   TEXT NOT NULL,
    first_name      TEXT NOT NULL,
    last_name       TEXT NOT NULL,
    is_active       BOOLEAN NOT NULL DEFAULT true,
    password_reset_hash    TEXT,
    password_reset_expires TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deactivated_at  TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_staff_users_email ON staff_users (lower(email));

-- One row per (role, scope) grant. A user may hold different roles at
-- different branches (RBAC-02) — permissions resolve per request against
-- the branch/franchise in scope, never against one flattened role.
-- franchise_id/branch_id are NOT foreign-keyed yet: the tenancy module
-- (franchises/branches tables) lands in a later migration and adds the
-- constraints then.
CREATE TABLE staff_role_assignments (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    staff_user_id   UUID NOT NULL REFERENCES staff_users(id) ON DELETE CASCADE,
    role            TEXT NOT NULL,           -- super_admin | finance | franchise_owner | franchise_viewer
                                              -- | branch_manager | counter_staff | kitchen
    scope_type      TEXT NOT NULL,           -- brand | franchise | branch
    franchise_id    UUID,
    branch_id       UUID,
    max_refund_cents      INTEGER,           -- RBAC-03 numeric limits
    max_discount_pct      NUMERIC(5,2),
    order_void_window_mins INTEGER,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_scope_consistency CHECK (
        (scope_type = 'brand'     AND franchise_id IS NULL AND branch_id IS NULL) OR
        (scope_type = 'franchise' AND franchise_id IS NOT NULL AND branch_id IS NULL) OR
        (scope_type = 'branch'    AND branch_id IS NOT NULL)
    )
);

CREATE INDEX idx_staff_role_assignments_user ON staff_role_assignments (staff_user_id);
CREATE INDEX idx_staff_role_assignments_franchise ON staff_role_assignments (franchise_id);
CREATE INDEX idx_staff_role_assignments_branch ON staff_role_assignments (branch_id);

-- +goose Down
DROP TABLE IF EXISTS staff_role_assignments;
DROP TABLE IF EXISTS staff_users;
