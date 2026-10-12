-- +goose Up
-- +goose StatementBegin

/*
 * Kitchen Display System (KDS).
 *
 * Everything here is additive. Existing tables gain columns with defaults, so
 * every insert path that predates the KDS (storefront checkout, POS, the
 * admin's manual order) keeps working unchanged and simply starts its orders
 * in the kitchen queue.
 */

-- ---------------------------------------------------------------------------
-- Central kitchens ("Corporate Offices" in the console).
--
-- A kitchen that produces for several stores. It has its own Location ID
-- (business_id) that a KDS signs in with, and a set of stores it serves.
-- ---------------------------------------------------------------------------
CREATE TABLE central_kitchens (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name           TEXT NOT NULL,
    business_id    TEXT NOT NULL,
    street         TEXT NOT NULL DEFAULT '',
    city           TEXT NOT NULL DEFAULT '',
    state          TEXT NOT NULL DEFAULT '',
    postcode       TEXT NOT NULL DEFAULT '',
    country        TEXT NOT NULL DEFAULT 'US',
    phone          TEXT,
    email          TEXT,
    manager_name   TEXT,
    manager_phone  TEXT,
    manager_email  TEXT,
    timezone       TEXT NOT NULL DEFAULT 'America/New_York',
    status         TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    created_by     UUID,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at     TIMESTAMPTZ
);

CREATE UNIQUE INDEX central_kitchens_business_id_idx
    ON central_kitchens (upper(btrim(business_id))) WHERE deleted_at IS NULL;
CREATE INDEX central_kitchens_status_idx ON central_kitchens (status) WHERE deleted_at IS NULL;

-- The stores a central kitchen produces for ("Store Assignment").
CREATE TABLE central_kitchen_branches (
    central_kitchen_id UUID NOT NULL REFERENCES central_kitchens(id) ON DELETE CASCADE,
    branch_id          UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (central_kitchen_id, branch_id)
);
CREATE INDEX central_kitchen_branches_branch_idx ON central_kitchen_branches (branch_id);

/*
 * A Location ID must name exactly one place.
 *
 * Branches and central kitchens keep their codes in different tables, and a
 * unique index cannot span two tables — so the rule is a trigger on each side.
 * The application checks first and says which field clashed; this is the
 * backstop for anything that gets past it.
 */
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

CREATE TRIGGER branches_business_id_cross_unique
    BEFORE INSERT OR UPDATE OF business_id ON branches
    FOR EACH ROW EXECUTE FUNCTION kds_assert_business_id_unique();

CREATE TRIGGER central_kitchens_business_id_cross_unique
    BEFORE INSERT OR UPDATE OF business_id ON central_kitchens
    FOR EACH ROW EXECUTE FUNCTION kds_assert_business_id_unique();

-- ---------------------------------------------------------------------------
-- Staff scoped to a central kitchen.
-- ---------------------------------------------------------------------------
ALTER TABLE staff_role_assignments
    ADD COLUMN IF NOT EXISTS central_kitchen_id UUID REFERENCES central_kitchens(id);
CREATE INDEX IF NOT EXISTS idx_staff_role_assignments_central_kitchen
    ON staff_role_assignments (central_kitchen_id);

ALTER TABLE staff_role_assignments DROP CONSTRAINT IF EXISTS chk_scope_consistency;
ALTER TABLE staff_role_assignments
    ADD CONSTRAINT chk_scope_consistency CHECK (
        (scope_type = 'brand'           AND branch_id IS NULL     AND central_kitchen_id IS NULL) OR
        (scope_type = 'branch'          AND branch_id IS NOT NULL AND central_kitchen_id IS NULL) OR
        (scope_type = 'central_kitchen' AND branch_id IS NULL     AND central_kitchen_id IS NOT NULL)
    ) NOT VALID;

-- ---------------------------------------------------------------------------
-- KDS settings, one row per location (a store or a central kitchen).
-- ---------------------------------------------------------------------------
CREATE TABLE kds_settings (
    id                          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    scope_type                  TEXT NOT NULL CHECK (scope_type IN ('branch', 'central_kitchen')),
    branch_id                   UUID REFERENCES branches(id) ON DELETE CASCADE,
    central_kitchen_id          UUID REFERENCES central_kitchens(id) ON DELETE CASCADE,

    -- Off by default: see the note on kds_enabled in franchiseapi.KDSSettings.
    -- While off, the POS closes an order on payment exactly as it always has.
    kds_enabled                 BOOLEAN NOT NULL DEFAULT FALSE,

    -- Order & Fulfillment
    show_pickup                 BOOLEAN NOT NULL DEFAULT TRUE,
    show_delivery               BOOLEAN NOT NULL DEFAULT TRUE,
    show_takeaway               BOOLEAN NOT NULL DEFAULT TRUE,
    show_orders_based_on_time   BOOLEAN NOT NULL DEFAULT TRUE,
    order_appear_minutes        INTEGER NOT NULL DEFAULT 15 CHECK (order_appear_minutes >= 0),
    play_sound_on_new_ticket    BOOLEAN NOT NULL DEFAULT TRUE,

    -- Ticket customization
    ticket_layout               TEXT NOT NULL DEFAULT 'one_rail'
                                CHECK (ticket_layout IN ('one_rail', 'two_rail', 'flex_rail', 'tile_rail')),
    ticket_columns              INTEGER NOT NULL DEFAULT 4 CHECK (ticket_columns IN (2, 4, 6)),
    header_colors_enabled       BOOLEAN NOT NULL DEFAULT TRUE,
    pickup_color                TEXT NOT NULL DEFAULT 'green',
    delivery_color              TEXT NOT NULL DEFAULT 'purple',
    takeaway_color              TEXT NOT NULL DEFAULT 'blue',
    dark_theme                  BOOLEAN NOT NULL DEFAULT TRUE,

    -- Calendar settings: order-count ranges for the Low / Medium / High labels.
    calendar_low_from           INTEGER NOT NULL DEFAULT 1,
    calendar_low_to             INTEGER NOT NULL DEFAULT 3,
    calendar_medium_from        INTEGER NOT NULL DEFAULT 4,
    calendar_medium_to          INTEGER NOT NULL DEFAULT 8,
    calendar_high_from          INTEGER NOT NULL DEFAULT 9,

    created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT kds_settings_scope_check CHECK (
        (scope_type = 'branch'          AND branch_id IS NOT NULL AND central_kitchen_id IS NULL) OR
        (scope_type = 'central_kitchen' AND central_kitchen_id IS NOT NULL AND branch_id IS NULL)
    )
);
CREATE UNIQUE INDEX kds_settings_branch_idx ON kds_settings (branch_id) WHERE branch_id IS NOT NULL;
CREATE UNIQUE INDEX kds_settings_central_kitchen_idx ON kds_settings (central_kitchen_id) WHERE central_kitchen_id IS NOT NULL;

-- Every existing store gets its settings row now, so a GET never 404s.
INSERT INTO kds_settings (scope_type, branch_id)
SELECT 'branch', b.id FROM branches b
ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- KDS printers. Same shape as pos_printers, scoped to a store or a kitchen.
-- ---------------------------------------------------------------------------
CREATE TABLE kds_printers (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    scope_type          TEXT NOT NULL CHECK (scope_type IN ('branch', 'central_kitchen')),
    branch_id           UUID REFERENCES branches(id) ON DELETE CASCADE,
    central_kitchen_id  UUID REFERENCES central_kitchens(id) ON DELETE CASCADE,
    name                TEXT NOT NULL,
    kind                TEXT NOT NULL DEFAULT 'wifi' CHECK (kind IN ('wifi', 'lan', 'bluetooth')),
    ip_address          TEXT,
    port                INTEGER NOT NULL DEFAULT 9100 CHECK (port BETWEEN 1 AND 65535),
    device_address      TEXT,
    is_connected        BOOLEAN NOT NULL DEFAULT FALSE,
    created_by          UUID,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT kds_printers_scope_check CHECK (
        (scope_type = 'branch'          AND branch_id IS NOT NULL AND central_kitchen_id IS NULL) OR
        (scope_type = 'central_kitchen' AND central_kitchen_id IS NOT NULL AND branch_id IS NULL)
    ),
    CONSTRAINT kds_printers_network_needs_ip CHECK (
        kind = 'bluetooth' OR (ip_address IS NOT NULL AND length(btrim(ip_address)) > 0)
    )
);
CREATE UNIQUE INDEX kds_printers_scope_name_idx
    ON kds_printers (COALESCE(branch_id, central_kitchen_id), lower(btrim(name)));
CREATE UNIQUE INDEX kds_printers_scope_ip_idx
    ON kds_printers (COALESCE(branch_id, central_kitchen_id), lower(btrim(ip_address)))
    WHERE kind IN ('wifi', 'lan') AND ip_address IS NOT NULL;

-- ---------------------------------------------------------------------------
-- Kitchen (KOT) status on orders and order lines.
-- ---------------------------------------------------------------------------
ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS kot_status TEXT NOT NULL DEFAULT 'queue',
    ADD COLUMN IF NOT EXISTS kot_started_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS kot_ready_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS kot_completed_at TIMESTAMPTZ,
    -- Changed by POS/admin while the kitchen had it; cleared by the KDS
    -- acknowledging it.
    ADD COLUMN IF NOT EXISTS is_amended BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS amended_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS amended_acknowledged BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS amended_acknowledged_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS amended_acknowledged_by UUID;

ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_kot_status_check;
ALTER TABLE orders ADD CONSTRAINT orders_kot_status_check CHECK (kot_status IN (
    'queue', 'preparing', 'ready', 'packing', 'ready_for_delivery',
    'out_for_delivery', 'delivered', 'completed', 'cancelled'));

-- Existing orders: derive the kitchen state from where the order already is,
-- so history reads sensibly on the KDS instead of every past order sitting in
-- the queue.
UPDATE orders SET kot_status = CASE status
        WHEN 'in_production'    THEN 'preparing'
        WHEN 'ready'            THEN 'ready'
        WHEN 'out_for_delivery' THEN 'out_for_delivery'
        WHEN 'fulfilled'        THEN 'completed'
        WHEN 'returned'         THEN 'completed'
        WHEN 'refunded'         THEN 'completed'
        WHEN 'cancelled'        THEN 'cancelled'
        ELSE 'queue' END
 WHERE kot_status = 'queue';

CREATE INDEX IF NOT EXISTS idx_orders_kot_status ON orders (kot_status);
CREATE INDEX IF NOT EXISTS idx_orders_branch_scheduled ON orders (branch_id, scheduled_date);

ALTER TABLE order_items
    ADD COLUMN IF NOT EXISTS kot_status TEXT NOT NULL DEFAULT 'queue',
    ADD COLUMN IF NOT EXISTS kot_started_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS kot_ready_at TIMESTAMPTZ;

ALTER TABLE order_items DROP CONSTRAINT IF EXISTS order_items_kot_status_check;
ALTER TABLE order_items ADD CONSTRAINT order_items_kot_status_check CHECK (kot_status IN (
    'queue', 'preparing', 'ready', 'completed', 'cancelled'));

UPDATE order_items oi SET kot_status = CASE
        WHEN oi.production_status IN ('cancelled', 'cancel') THEN 'cancelled'
        WHEN oi.production_status = 'done'                    THEN 'ready'
        WHEN oi.production_status = 'in_progress'             THEN 'preparing'
        WHEN o.kot_status IN ('ready', 'packing', 'ready_for_delivery',
                              'out_for_delivery', 'delivered', 'completed') THEN 'ready'
        ELSE 'queue' END
  FROM orders o
 WHERE o.id = oi.order_id AND oi.kot_status = 'queue';

-- The kitchen timeline: every KOT move, for the order or one line.
CREATE TABLE order_kot_events (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id       UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    order_item_id  UUID REFERENCES order_items(id) ON DELETE CASCADE,
    from_status    TEXT,
    to_status      TEXT NOT NULL,
    actor_id       UUID,
    actor_type     TEXT NOT NULL DEFAULT 'staff',
    source         TEXT NOT NULL DEFAULT 'kds',   -- kds | pos | admin | system
    note           TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX order_kot_events_order_idx ON order_kot_events (order_id, created_at);

-- ---------------------------------------------------------------------------
-- Product preparation time: a minimum and a maximum, each with its unit.
-- lead_time/lead_time_unit are untouched and keep their meaning.
-- ---------------------------------------------------------------------------
ALTER TABLE products
    ADD COLUMN IF NOT EXISTS min_prep_time INTEGER NOT NULL DEFAULT 0 CHECK (min_prep_time >= 0),
    ADD COLUMN IF NOT EXISTS min_prep_time_unit TEXT NOT NULL DEFAULT 'minutes'
        CHECK (min_prep_time_unit IN ('minutes', 'hours')),
    ADD COLUMN IF NOT EXISTS max_prep_time INTEGER NOT NULL DEFAULT 0 CHECK (max_prep_time >= 0),
    ADD COLUMN IF NOT EXISTS max_prep_time_unit TEXT NOT NULL DEFAULT 'minutes'
        CHECK (max_prep_time_unit IN ('minutes', 'hours'));

-- Seed from the single lead time products carried until now, so existing
-- products have a sensible window before anybody edits them.
UPDATE products SET
    min_prep_time = CASE WHEN lower(lead_time_unit) LIKE 'day%' THEN lead_time * 24 ELSE lead_time END,
    min_prep_time_unit = CASE WHEN lower(lead_time_unit) LIKE 'min%' THEN 'minutes' ELSE 'hours' END,
    max_prep_time = CASE WHEN lower(lead_time_unit) LIKE 'day%' THEN lead_time * 24 ELSE lead_time END,
    max_prep_time_unit = CASE WHEN lower(lead_time_unit) LIKE 'min%' THEN 'minutes' ELSE 'hours' END
 WHERE min_prep_time = 0 AND max_prep_time = 0 AND lead_time > 0;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE products
    DROP COLUMN IF EXISTS max_prep_time_unit,
    DROP COLUMN IF EXISTS max_prep_time,
    DROP COLUMN IF EXISTS min_prep_time_unit,
    DROP COLUMN IF EXISTS min_prep_time;

DROP TABLE IF EXISTS order_kot_events;

ALTER TABLE order_items DROP CONSTRAINT IF EXISTS order_items_kot_status_check;
ALTER TABLE order_items
    DROP COLUMN IF EXISTS kot_ready_at,
    DROP COLUMN IF EXISTS kot_started_at,
    DROP COLUMN IF EXISTS kot_status;

DROP INDEX IF EXISTS idx_orders_branch_scheduled;
DROP INDEX IF EXISTS idx_orders_kot_status;
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_kot_status_check;
ALTER TABLE orders
    DROP COLUMN IF EXISTS amended_acknowledged_by,
    DROP COLUMN IF EXISTS amended_acknowledged_at,
    DROP COLUMN IF EXISTS amended_acknowledged,
    DROP COLUMN IF EXISTS amended_at,
    DROP COLUMN IF EXISTS is_amended,
    DROP COLUMN IF EXISTS kot_completed_at,
    DROP COLUMN IF EXISTS kot_ready_at,
    DROP COLUMN IF EXISTS kot_started_at,
    DROP COLUMN IF EXISTS kot_status;

DROP TABLE IF EXISTS kds_printers;
DROP TABLE IF EXISTS kds_settings;

ALTER TABLE staff_role_assignments DROP CONSTRAINT IF EXISTS chk_scope_consistency;
ALTER TABLE staff_role_assignments
    ADD CONSTRAINT chk_scope_consistency CHECK (
        (scope_type = 'brand'  AND branch_id IS NULL) OR
        (scope_type = 'branch' AND branch_id IS NOT NULL)
    ) NOT VALID;
DROP INDEX IF EXISTS idx_staff_role_assignments_central_kitchen;
ALTER TABLE staff_role_assignments DROP COLUMN IF EXISTS central_kitchen_id;

DROP TRIGGER IF EXISTS central_kitchens_business_id_cross_unique ON central_kitchens;
DROP TRIGGER IF EXISTS branches_business_id_cross_unique ON branches;
DROP FUNCTION IF EXISTS kds_assert_business_id_unique();

DROP TABLE IF EXISTS central_kitchen_branches;
DROP TABLE IF EXISTS central_kitchens;
-- +goose StatementEnd
