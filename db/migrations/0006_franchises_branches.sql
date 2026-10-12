-- +goose Up

-- A company-owned location is modelled as a franchise whose owner is the
-- brand (is_company_owned = true) — one code path for every location,
-- company-owned or franchised (BRD Section 5.1).
CREATE TABLE franchises (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name              TEXT NOT NULL,
    legal_entity      TEXT NOT NULL,
    primary_contact   JSONB NOT NULL DEFAULT '{}', -- {name, email, phone}
    contract_ref      TEXT,
    commercial_terms  JSONB NOT NULL DEFAULT '{}', -- {royaltyRate, feeBasis, paymentTerms}
    territory         TEXT,
    status            TEXT NOT NULL DEFAULT 'pending'
                      CHECK (status IN ('pending','active','suspended','closed')),
    is_company_owned  BOOLEAN NOT NULL DEFAULT false,
    onboarded_at      TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE branches (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    franchise_id        UUID NOT NULL REFERENCES franchises(id),
    name                TEXT NOT NULL,
    code                TEXT UNIQUE NOT NULL,
    type                TEXT NOT NULL DEFAULT 'retail' CHECK (type IN ('retail','central_kitchen')),

    street              TEXT NOT NULL,
    city                TEXT NOT NULL,
    state               TEXT NOT NULL,
    postcode            TEXT NOT NULL,
    country             TEXT NOT NULL DEFAULT 'US',
    latitude            DOUBLE PRECISION,
    longitude           DOUBLE PRECISION,

    timezone            TEXT NOT NULL DEFAULT 'America/New_York', -- named zone (GOV-07): DST-safe
    opening_hours       JSONB NOT NULL DEFAULT '[]',              -- [{day,open,close}]

    fulfilment_types    TEXT[] NOT NULL DEFAULT ARRAY['pickup','delivery'],
    delivery_zone       JSONB NOT NULL DEFAULT '{"type":"zip_list","zips":[]}',
    delivery_fee_cents  INTEGER NOT NULL DEFAULT 0,
    min_order_cents     INTEGER NOT NULL DEFAULT 0,

    tax_state           TEXT,
    tax_county           TEXT,
    tax_rate_pct         NUMERIC(6,4),

    status              TEXT NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending','active','suspended','closed')),
    emergency_closed_on  DATE, -- set for "today" only; resets at midnight branch-local time

    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_branches_franchise ON branches (franchise_id);
CREATE INDEX idx_branches_status ON branches (status);

-- staff_role_assignments.franchise_id/branch_id were left unconstrained in
-- 0004 because these tables did not exist yet — add the real FKs now.
-- NOT VALID: enforced for every new/updated row from here on, without
-- requiring pre-existing rows (e.g. seed/test data created before this
-- migration ran) to already satisfy it.
ALTER TABLE staff_role_assignments
    ADD CONSTRAINT fk_staff_role_assignments_franchise FOREIGN KEY (franchise_id) REFERENCES franchises(id) NOT VALID,
    ADD CONSTRAINT fk_staff_role_assignments_branch FOREIGN KEY (branch_id) REFERENCES branches(id) NOT VALID;

-- audit_log.franchise_id/branch_id are similarly unconstrained by design —
-- audit history must survive even if a branch record is ever hard-deleted,
-- so no FK is added there.

-- +goose Down
ALTER TABLE staff_role_assignments
    DROP CONSTRAINT IF EXISTS fk_staff_role_assignments_branch,
    DROP CONSTRAINT IF EXISTS fk_staff_role_assignments_franchise;
DROP TABLE IF EXISTS branches;
DROP TABLE IF EXISTS franchises;
