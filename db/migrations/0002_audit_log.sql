-- +goose Up

-- audit_log is written by every permission-gated mutation across every
-- module (RBAC-05) and is never updated or deleted, even by super admin
-- (NFR-11, data-model invariant in the BRD). The trigger below is a
-- second line of defense behind the application-layer rule.
CREATE TABLE audit_log (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id      UUID,
    actor_type    TEXT NOT NULL,            -- customer | staff | system
    actor_role    TEXT,
    action        TEXT NOT NULL,            -- e.g. "order.amend", "branch.suspend"
    resource_type TEXT NOT NULL,
    resource_id   UUID,
    franchise_id  UUID,
    branch_id     UUID,
    before_value  JSONB,
    after_value   JSONB,
    reason        TEXT,
    ip_address    TEXT,
    device_id     UUID,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_log_resource ON audit_log (resource_type, resource_id);
CREATE INDEX idx_audit_log_branch ON audit_log (branch_id, created_at DESC);
CREATE INDEX idx_audit_log_franchise ON audit_log (franchise_id, created_at DESC);
CREATE INDEX idx_audit_log_actor ON audit_log (actor_id, created_at DESC);

-- +goose StatementBegin
CREATE FUNCTION audit_log_append_only() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'audit_log is append-only: % is not permitted', TG_OP;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_audit_log_no_update
    BEFORE UPDATE ON audit_log
    FOR EACH ROW EXECUTE FUNCTION audit_log_append_only();

CREATE TRIGGER trg_audit_log_no_delete
    BEFORE DELETE ON audit_log
    FOR EACH ROW EXECUTE FUNCTION audit_log_append_only();

-- +goose Down
DROP TRIGGER IF EXISTS trg_audit_log_no_delete ON audit_log;
DROP TRIGGER IF EXISTS trg_audit_log_no_update ON audit_log;
DROP FUNCTION IF EXISTS audit_log_append_only();
DROP TABLE IF EXISTS audit_log;
