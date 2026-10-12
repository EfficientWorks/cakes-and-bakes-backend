-- +goose Up

-- Customer accounts are brand-level, not branch-level (ACC-17): one
-- registration works across every branch in the network.
CREATE TABLE customers (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email                   TEXT UNIQUE NOT NULL,
    password_hash           TEXT NOT NULL,
    first_name              TEXT NOT NULL,
    last_name               TEXT NOT NULL,
    mobile_number           TEXT,
    is_verified             BOOLEAN NOT NULL DEFAULT false,
    otp_hash                TEXT,
    otp_expires_at          TIMESTAMPTZ,
    otp_attempts            INTEGER NOT NULL DEFAULT 0,
    marketing_opt_in        BOOLEAN NOT NULL DEFAULT false,
    marketing_opt_in_at     TIMESTAMPTZ,
    marketing_opt_in_ip     TEXT,
    password_reset_hash     TEXT,
    password_reset_expires  TIMESTAMPTZ,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at               TIMESTAMPTZ  -- soft delete; orders retained anonymised (ACC-05, NFR-08)
);

CREATE UNIQUE INDEX idx_customers_email ON customers (lower(email)) WHERE deleted_at IS NULL;

CREATE TABLE customer_addresses (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id  UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    label        TEXT,
    street       TEXT NOT NULL,
    city         TEXT NOT NULL,
    state        TEXT NOT NULL,
    postcode     TEXT NOT NULL,
    country      TEXT NOT NULL DEFAULT 'US',
    is_default   BOOLEAN NOT NULL DEFAULT false,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_customer_addresses_customer ON customer_addresses (customer_id);

CREATE TABLE customer_recipients (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id     UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    first_name      TEXT NOT NULL,
    last_name       TEXT NOT NULL,
    phone           TEXT NOT NULL,
    relationship    TEXT,
    occasion_dates  JSONB NOT NULL DEFAULT '[]',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_customer_recipients_customer ON customer_recipients (customer_id);

-- Shared refresh-token store for both auth realms (customer + staff).
-- Access tokens are stateless JWTs; refresh tokens are opaque and revocable.
CREATE TABLE refresh_tokens (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    subject_type  TEXT NOT NULL,             -- customer | staff
    subject_id    UUID NOT NULL,
    token_hash    TEXT NOT NULL,
    expires_at    TIMESTAMPTZ NOT NULL,
    revoked_at    TIMESTAMPTZ,
    replaced_by   UUID REFERENCES refresh_tokens(id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_ip    TEXT
);

CREATE INDEX idx_refresh_tokens_subject ON refresh_tokens (subject_type, subject_id);
CREATE UNIQUE INDEX idx_refresh_tokens_hash ON refresh_tokens (token_hash);

-- +goose Down
DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS customer_recipients;
DROP TABLE IF EXISTS customer_addresses;
DROP TABLE IF EXISTS customers;
