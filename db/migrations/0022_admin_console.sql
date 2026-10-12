-- +goose Up

-- Everything the admin console needs that the schema did not yet carry:
-- scheduled categories, a reusable modifier library, and coupons.
--
-- See backed-bouquet-admin/docs/api-gaps.md for why each of these exists —
-- the console screens were built against them before the API had them.

-- ---------------------------------------------------------------------------
-- Categories: scheduling (CAT-09)
-- ---------------------------------------------------------------------------

-- A category can now switch itself on and off on set dates, the same way a
-- product already could through branch_product_rules. Null on both means
-- "always on", which is what every existing row becomes.
ALTER TABLE product_categories
    ADD COLUMN available_from  DATE,
    ADD COLUMN available_until DATE;

-- ---------------------------------------------------------------------------
-- Modifier groups become a reusable library (the "Apply To Items" flow)
-- ---------------------------------------------------------------------------

-- Until now a modifier group belonged to exactly one product, so "Size" had to
-- be recreated for every bouquet. The console treats groups as a library —
-- authored once, applied to many products — which needs two changes:
--
--   1. product_id becomes nullable. A library group belongs to no product.
--   2. a join table carries the many-to-many, with its own per-product order.
--
-- Existing product-owned groups are migrated into the join table below and
-- keep working unchanged: the read path unions both shapes, so nothing that
-- reads modifier_groups today has to know this happened.
ALTER TABLE modifier_groups ALTER COLUMN product_id DROP NOT NULL;

-- The customer-facing label, as distinct from the internal name the admin
-- list and search use. "Size" internally, "Pick a size" on the product page.
ALTER TABLE modifier_groups ADD COLUMN display_name TEXT;
UPDATE modifier_groups SET display_name = name WHERE display_name IS NULL;

-- The console divides modifiers into exactly two kinds — variations (pick one
-- from a set) and add-ons (add any number of extras) — because they are
-- different objects with different forms and different meaning on the product
-- page. The existing `type` column is a finer-grained taxonomy
-- (flavour/addon/presentation/wrap_colour); this maps it onto the two the UI
-- works in without discarding it.
ALTER TABLE modifier_groups
    ADD COLUMN kind TEXT NOT NULL DEFAULT 'addon'
        CHECK (kind IN ('variation', 'addon'));

-- Wrap colour and presentation are single-choice, so they are variations;
-- flavours and add-ons are multi-select extras.
UPDATE modifier_groups SET kind = 'variation' WHERE type IN ('presentation', 'wrap_colour');

-- Add-ons may allow more than one selection; variations never do.
ALTER TABLE modifier_groups ADD COLUMN allow_multiple BOOLEAN NOT NULL DEFAULT false;
UPDATE modifier_groups SET allow_multiple = true WHERE kind = 'addon' AND max_select <> 1;

-- max_select NULL means "no maximum", which the console offers explicitly and
-- the old NOT NULL DEFAULT 1 could not express.
ALTER TABLE modifier_groups ALTER COLUMN max_select DROP NOT NULL;

-- Exactly one option per variation group is pre-selected — what the product
-- page loads with. Enforced in the application, since a partial unique index
-- cannot express "at most one true per group" across an update that flips two
-- rows in one statement.
ALTER TABLE modifier_options ADD COLUMN is_pre_selected BOOLEAN NOT NULL DEFAULT false;

CREATE TABLE product_modifier_groups (
    product_id        UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    modifier_group_id UUID NOT NULL REFERENCES modifier_groups(id) ON DELETE CASCADE,
    sort_order        INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (product_id, modifier_group_id)
);

CREATE INDEX idx_pmg_product ON product_modifier_groups (product_id, sort_order);
CREATE INDEX idx_pmg_group ON product_modifier_groups (modifier_group_id);

-- Carry every existing product-owned group across, so the join table is the
-- single place the read path has to look from here on.
INSERT INTO product_modifier_groups (product_id, modifier_group_id, sort_order)
SELECT product_id, id, sort_order FROM modifier_groups WHERE product_id IS NOT NULL
ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- Coupons (ADM-23 local promotions, SA-13 network-wide promotions)
-- ---------------------------------------------------------------------------

CREATE TABLE coupons (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code                TEXT NOT NULL,
    name                TEXT NOT NULL,

    -- percentage: discount_value is whole percent.
    -- fixed:      discount_value is integer cents (core invariant 4).
    -- free_*:     discount_value is ignored; the fee itself is waived.
    type                TEXT NOT NULL CHECK (type IN ('percentage','fixed','free_shipping','free_delivery')),
    discount_value      INTEGER NOT NULL DEFAULT 0,

    status              TEXT NOT NULL DEFAULT 'active'
                          CHECK (status IN ('active','scheduled','paused','expired')),

    applies_to          TEXT NOT NULL DEFAULT 'all' CHECK (applies_to IN ('all','category','product')),
    target_ids          UUID[] NOT NULL DEFAULT '{}',

    -- NULL = unlimited. redeemed_count is maintained by the redemption ledger
    -- below rather than trusted from a client.
    usage_limit         INTEGER,
    redeemed_count      INTEGER NOT NULL DEFAULT 0,

    has_active_dates    BOOLEAN NOT NULL DEFAULT false,
    starts_at           TIMESTAMPTZ,
    ends_at             TIMESTAMPTZ,

    has_minimum_order   BOOLEAN NOT NULL DEFAULT false,
    minimum_order_cents INTEGER NOT NULL DEFAULT 0,

    -- NULL scopes the coupon to the whole network (brand-level, SA-13).
    -- A branch id makes it a local promotion (franchise-level, ADM-23), and
    -- the governance level is enforced on write accordingly.
    branch_id           UUID REFERENCES branches(id) ON DELETE CASCADE,

    created_by          UUID,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at          TIMESTAMPTZ
);

-- A code is unique within its scope: one network-wide SUMMER25, or one per
-- branch. Two partial indexes rather than one, because NULL never equals NULL
-- in a composite unique index and a single index would let two network-wide
-- coupons share a code.
CREATE UNIQUE INDEX idx_coupons_code_network ON coupons (upper(code))
    WHERE branch_id IS NULL AND deleted_at IS NULL;
CREATE UNIQUE INDEX idx_coupons_code_branch ON coupons (branch_id, upper(code))
    WHERE branch_id IS NOT NULL AND deleted_at IS NULL;

CREATE INDEX idx_coupons_branch ON coupons (branch_id) WHERE deleted_at IS NULL;

-- One row per successful redemption. This is what makes redeemed_count
-- trustworthy and what stops a usage limit being exceeded by two concurrent
-- checkouts — the insert and the count both happen inside the order
-- transaction.
CREATE TABLE coupon_redemptions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    coupon_id   UUID NOT NULL REFERENCES coupons(id) ON DELETE CASCADE,
    order_id    UUID NOT NULL,
    customer_id UUID,
    amount_cents INTEGER NOT NULL,
    redeemed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- An order can only carry one coupon, and replaying the same order must
    -- not double-count it.
    UNIQUE (order_id)
);

CREATE INDEX idx_coupon_redemptions_coupon ON coupon_redemptions (coupon_id);

-- +goose Down

DROP TABLE IF EXISTS coupon_redemptions;
DROP TABLE IF EXISTS coupons;
DROP TABLE IF EXISTS product_modifier_groups;

ALTER TABLE modifier_options DROP COLUMN IF EXISTS is_pre_selected;
ALTER TABLE modifier_groups
    DROP COLUMN IF EXISTS allow_multiple,
    DROP COLUMN IF EXISTS kind,
    DROP COLUMN IF EXISTS display_name;
ALTER TABLE modifier_groups ALTER COLUMN max_select SET NOT NULL;
-- product_id is deliberately left nullable on the way down: a library group
-- has no product to point at, so restoring NOT NULL would fail on real data.

ALTER TABLE product_categories
    DROP COLUMN IF EXISTS available_until,
    DROP COLUMN IF EXISTS available_from;
