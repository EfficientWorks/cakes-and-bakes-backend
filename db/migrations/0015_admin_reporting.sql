-- +goose Up

-- Reporting queries (admin/reporting module) scan orders/refunds/audit_log
-- by time range across the whole network, a pattern not served by the
-- existing branch/franchise-scoped indexes from 0013/0014/0002.
CREATE INDEX idx_orders_created_at ON orders (created_at);
CREATE INDEX idx_refunds_created_at ON refunds (created_at);
CREATE INDEX idx_audit_log_created_at ON audit_log (created_at DESC);

-- Franchise/branch owner freeform notes and labels on a customer record
-- (ADM-19). Customer accounts are brand-level (0003), so these are not
-- franchise-scoped either — visibility into which customers a caller may
-- see is derived from order history, enforced in the service layer.
CREATE TABLE customer_notes (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id     UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    author_staff_id UUID NOT NULL REFERENCES staff_users(id),
    note            TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_customer_notes_customer ON customer_notes (customer_id, created_at DESC);

CREATE TABLE customer_tags (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id     UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    tag             TEXT NOT NULL,
    created_by      UUID REFERENCES staff_users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (customer_id, tag)
);

CREATE INDEX idx_customer_tags_customer ON customer_tags (customer_id);

-- +goose Down
DROP TABLE IF EXISTS customer_tags;
DROP TABLE IF EXISTS customer_notes;
DROP INDEX IF EXISTS idx_audit_log_created_at;
DROP INDEX IF EXISTS idx_refunds_created_at;
DROP INDEX IF EXISTS idx_orders_created_at;
