-- +goose Up

-- Guest storefront checkouts need a recipient book keyed by the purchaser's
-- email, independently of whether the purchaser has a customer account.
--
-- Idempotent: the table already existed in the live database without a
-- matching goose_db_version row, so a plain CREATE TABLE failed on deploy.
CREATE TABLE IF NOT EXISTS storefront_recipients (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    purchaser_email TEXT NOT NULL,
    recipient_email TEXT NOT NULL,
    first_name      TEXT NOT NULL,
    last_name       TEXT NOT NULL,
    phone           TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (purchaser_email, recipient_email)
);

CREATE INDEX IF NOT EXISTS idx_storefront_recipients_purchaser_email
    ON storefront_recipients (purchaser_email);

-- +goose Down
DROP TABLE IF EXISTS storefront_recipients;