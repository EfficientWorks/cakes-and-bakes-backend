-- +goose Up
-- +goose StatementBegin

/*
 * Businesses: the companies that use emzo Bakehouse.
 *
 * A business (for example a bakery chain) is registered by the emzo team
 * through the platform API, gets an owner login, and then creates its own
 * stores, staff and catalogue in the admin console. Its country decides the
 * defaults below (currency, timezone, tax) when it is registered; each can be
 * changed afterwards.
 */
CREATE TABLE businesses (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name               TEXT NOT NULL,
    -- URL-safe handle, unique across the platform: used for the storefront
    -- address and to find a business without its id.
    slug               TEXT NOT NULL,
    country_code       TEXT NOT NULL CHECK (country_code ~ '^[A-Z]{2}$'),
    currency           TEXT NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    timezone           TEXT NOT NULL,
    locale             TEXT NOT NULL,
    -- inclusive: shelf prices already contain tax (UK VAT).
    -- exclusive: tax is added on top at checkout (US sales tax).
    tax_mode           TEXT NOT NULL CHECK (tax_mode IN ('inclusive', 'exclusive')),
    tax_name           TEXT NOT NULL,
    default_tax_rate_pct NUMERIC(5, 2) NOT NULL DEFAULT 0 CHECK (default_tax_rate_pct >= 0 AND default_tax_rate_pct <= 100),
    contact_email      TEXT,
    contact_phone      TEXT,
    status             TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended')),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_businesses_slug ON businesses (lower(slug));

/*
 * branches.business_id and central_kitchens.business_id held the short
 * "Location ID" a till or kitchen screen signs in with (e.g. LDN-001-4827).
 * They become location_code, so business_id can mean the business a row
 * belongs to. The POS/KDS API keeps its "businessId" field name.
 */
ALTER TABLE branches RENAME COLUMN business_id TO location_code;
ALTER TABLE central_kitchens RENAME COLUMN business_id TO location_code;
ALTER INDEX IF EXISTS branches_business_id_key RENAME TO branches_location_code_key;
ALTER INDEX IF EXISTS central_kitchens_business_id_idx RENAME TO central_kitchens_location_code_idx;

-- 0073's "one Location ID, one place" trigger names the column in its body,
-- which a rename does not rewrite. Replace it under the new names.
DROP TRIGGER IF EXISTS branches_business_id_cross_unique ON branches;
DROP TRIGGER IF EXISTS central_kitchens_business_id_cross_unique ON central_kitchens;
DROP FUNCTION IF EXISTS kds_assert_business_id_unique();

CREATE FUNCTION assert_location_code_unique() RETURNS trigger AS $$
BEGIN
    IF NEW.location_code IS NULL OR btrim(NEW.location_code) = '' THEN
        RETURN NEW;
    END IF;
    IF TG_TABLE_NAME = 'branches' THEN
        IF EXISTS (SELECT 1 FROM central_kitchens ck
                    WHERE ck.deleted_at IS NULL
                      AND upper(btrim(ck.location_code)) = upper(btrim(NEW.location_code))) THEN
            RAISE EXCEPTION 'location code % is already used by a central kitchen', NEW.location_code
                USING ERRCODE = '23505', CONSTRAINT = 'location_code_cross_location_unique';
        END IF;
    ELSE
        IF EXISTS (SELECT 1 FROM branches b
                    WHERE upper(btrim(b.location_code)) = upper(btrim(NEW.location_code))) THEN
            RAISE EXCEPTION 'location code % is already used by a store', NEW.location_code
                USING ERRCODE = '23505', CONSTRAINT = 'location_code_cross_location_unique';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER branches_location_code_cross_unique
    BEFORE INSERT OR UPDATE OF location_code ON branches
    FOR EACH ROW EXECUTE FUNCTION assert_location_code_unique();

CREATE TRIGGER central_kitchens_location_code_cross_unique
    BEFORE INSERT OR UPDATE OF location_code ON central_kitchens
    FOR EACH ROW EXECUTE FUNCTION assert_location_code_unique();

/*
 * Which business each row belongs to. Nullable while rows created before
 * businesses existed are assigned one; stores, kitchens and staff created
 * from now on always carry it.
 */
ALTER TABLE branches         ADD COLUMN business_id UUID REFERENCES businesses (id);
ALTER TABLE central_kitchens ADD COLUMN business_id UUID REFERENCES businesses (id);
ALTER TABLE staff_users      ADD COLUMN business_id UUID REFERENCES businesses (id);

CREATE INDEX idx_branches_business         ON branches (business_id);
CREATE INDEX idx_central_kitchens_business ON central_kitchens (business_id);
CREATE INDEX idx_staff_users_business      ON staff_users (business_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_staff_users_business;
DROP INDEX IF EXISTS idx_central_kitchens_business;
DROP INDEX IF EXISTS idx_branches_business;

ALTER TABLE staff_users      DROP COLUMN IF EXISTS business_id;
ALTER TABLE central_kitchens DROP COLUMN IF EXISTS business_id;
ALTER TABLE branches         DROP COLUMN IF EXISTS business_id;

DROP TRIGGER IF EXISTS central_kitchens_location_code_cross_unique ON central_kitchens;
DROP TRIGGER IF EXISTS branches_location_code_cross_unique ON branches;
DROP FUNCTION IF EXISTS assert_location_code_unique();

ALTER INDEX IF EXISTS central_kitchens_location_code_idx RENAME TO central_kitchens_business_id_idx;
ALTER INDEX IF EXISTS branches_location_code_key RENAME TO branches_business_id_key;
ALTER TABLE central_kitchens RENAME COLUMN location_code TO business_id;
ALTER TABLE branches RENAME COLUMN location_code TO business_id;

CREATE OR REPLACE FUNCTION kds_assert_business_id_unique() RETURNS trigger AS $$
BEGIN
    IF NEW.business_id IS NULL OR btrim(NEW.business_id) = '' THEN
        RETURN NEW;
    END IF;
    IF TG_TABLE_NAME = 'branches' THEN
        IF EXISTS (SELECT 1 FROM central_kitchens ck
                    WHERE ck.deleted_at IS NULL
                      AND upper(btrim(ck.business_id)) = upper(btrim(NEW.business_id))) THEN
            RAISE EXCEPTION 'business id % is already used by a central kitchen', NEW.business_id
                USING ERRCODE = '23505', CONSTRAINT = 'business_id_cross_location_unique';
        END IF;
    ELSE
        IF EXISTS (SELECT 1 FROM branches b
                    WHERE upper(btrim(b.business_id)) = upper(btrim(NEW.business_id))) THEN
            RAISE EXCEPTION 'business id % is already used by a store', NEW.business_id
                USING ERRCODE = '23505', CONSTRAINT = 'business_id_cross_location_unique';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS branches_business_id_cross_unique ON branches;
DROP TRIGGER IF EXISTS central_kitchens_business_id_cross_unique ON central_kitchens;

CREATE TRIGGER branches_business_id_cross_unique
    BEFORE INSERT OR UPDATE OF business_id ON branches
    FOR EACH ROW EXECUTE FUNCTION kds_assert_business_id_unique();

CREATE TRIGGER central_kitchens_business_id_cross_unique
    BEFORE INSERT OR UPDATE OF business_id ON central_kitchens
    FOR EACH ROW EXECUTE FUNCTION kds_assert_business_id_unique();

DROP TABLE IF EXISTS businesses;

-- +goose StatementEnd
