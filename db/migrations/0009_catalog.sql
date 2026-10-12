-- +goose Up

-- Three customer-facing categories only (CAT-01): Bouquets, Boxes, Macaron
-- Gift Boxes. Design themes are product variations, never separate
-- categories or separate products — that is the fix for pain point P4
-- (choice paralysis: 8 core products, not 20+).
CREATE TABLE product_categories (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    slug        TEXT UNIQUE NOT NULL,
    description TEXT,
    sort_order  INTEGER NOT NULL DEFAULT 0,
    is_active   BOOLEAN NOT NULL DEFAULT true
);

-- Master catalogue (governance: brand-locked — GOV-01, product.core_fields).
CREATE TABLE products (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    category_id          UUID NOT NULL REFERENCES product_categories(id),
    name                 TEXT NOT NULL,
    slug                 TEXT UNIQUE NOT NULL,
    description          TEXT NOT NULL DEFAULT '',
    base_price_cents     INTEGER NOT NULL,
    lead_time_hours      INTEGER NOT NULL DEFAULT 2,
    production_station   TEXT,               -- for KDS routing, e.g. "bouquet_station"
    allergen_flags       TEXT[] NOT NULL DEFAULT '{}',
    dietary_flags        TEXT[] NOT NULL DEFAULT '{}',
    occasion_tags        TEXT[] NOT NULL DEFAULT '{}',
    freshness_note       TEXT,
    ingredients_note     TEXT,
    returns_note         TEXT,
    is_best_seller       BOOLEAN NOT NULL DEFAULT false,
    is_active            BOOLEAN NOT NULL DEFAULT true,
    sort_order            INTEGER NOT NULL DEFAULT 0,
    price_floor_cents    INTEGER,            -- governance: franchise cannot price below this
    price_ceiling_cents  INTEGER,            -- governance: franchise cannot price above this
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at            TIMESTAMPTZ         -- soft delete only (admin DELETE)
);

CREATE INDEX idx_products_category ON products (category_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_products_occasion_tags ON products USING GIN (occasion_tags);

-- Product has more than one image, studio + customer photography (CX-07).
CREATE TABLE product_images (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id  UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    url         TEXT NOT NULL,
    alt_text    TEXT,
    type        TEXT NOT NULL DEFAULT 'studio' CHECK (type IN ('studio','customer')),
    sort_order  INTEGER NOT NULL DEFAULT 0,
    is_primary  BOOLEAN NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_product_images_product ON product_images (product_id, sort_order);

-- Size variants (6/9/12/15-count etc).
CREATE TABLE product_variants (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id        UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    name              TEXT NOT NULL,             -- "6 Cupcakes"
    label             TEXT,                      -- "Best Deal · Serves 3-4"
    price_delta_cents INTEGER NOT NULL DEFAULT 0,
    serves_min        INTEGER,
    serves_max        INTEGER,
    sort_order        INTEGER NOT NULL DEFAULT 0,
    is_active         BOOLEAN NOT NULL DEFAULT true
);

CREATE INDEX idx_product_variants_product ON product_variants (product_id, sort_order);

-- Modifier groups: Flavours, Add a little extra, Presentation, Wrap Colour.
CREATE TABLE modifier_groups (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id  UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    type        TEXT NOT NULL CHECK (type IN ('flavour','addon','presentation','wrap_colour')),
    is_required BOOLEAN NOT NULL DEFAULT false,
    min_select  INTEGER NOT NULL DEFAULT 0,
    max_select  INTEGER NOT NULL DEFAULT 1,
    sort_order  INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_modifier_groups_product ON modifier_groups (product_id, sort_order);

CREATE TABLE modifier_options (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    modifier_group_id UUID NOT NULL REFERENCES modifier_groups(id) ON DELETE CASCADE,
    name              TEXT NOT NULL,
    price_delta_cents INTEGER NOT NULL DEFAULT 0,
    colour_hex        TEXT,
    image_url         TEXT,
    allergen_flags    TEXT[] NOT NULL DEFAULT '{}',
    is_gluten_free    BOOLEAN NOT NULL DEFAULT false,
    is_active         BOOLEAN NOT NULL DEFAULT true,
    sort_order        INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_modifier_options_group ON modifier_options (modifier_group_id, sort_order);

-- Branch-level availability rules — the 4-layer cascade (CAT-04, Section 8.1).
-- Layer 1 (master catalogue) is products.is_active, evaluated in the app.
CREATE TABLE branch_product_rules (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id             UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    product_id            UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,

    branch_enabled        BOOLEAN NOT NULL DEFAULT true,                          -- layer 2
    available_days        TEXT[] NOT NULL DEFAULT ARRAY['monday','tuesday','wednesday','thursday','friday','saturday','sunday'], -- layer 3
    date_overrides         JSONB NOT NULL DEFAULT '[]',                            -- layer 4: [{date,available,reason}]

    price_override_cents  INTEGER,           -- within governance band; null = base price
    available_from        DATE,              -- seasonal window
    available_until       DATE,

    reason                TEXT,
    updated_by            UUID,
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (branch_id, product_id)
);

CREATE INDEX idx_branch_product_rules_branch ON branch_product_rules (branch_id);
CREATE INDEX idx_branch_product_rules_product ON branch_product_rules (product_id);

-- Admin-editable dynamic storefront content (hero headline, marquee, etc.) —
-- brand-locked, CMS-style, no deployment required to change (SF-35, SA-12).
CREATE TABLE site_content (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    type       TEXT NOT NULL DEFAULT 'text' CHECK (type IN ('text','html','json','image_url')),
    updated_by UUID,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO site_content (key, value, type) VALUES
    ('hero_headline',       'Sweet Gifts That Bloom with Joy', 'text'),
    ('hero_subtitle',       'Handcrafted to look like flowers, baked to taste amazing. Perfect for gifting, celebrations, and every special moment.', 'text'),
    ('marquee_text',        'NOW AVAILABLE | Elegant Cupcake Bouquets for Every Mom · Mother''s Day Collection is Now Available', 'text'),
    ('bestsellers_heading', 'Handpicked Favourites, Loved by Our Customers', 'text'),
    ('subscribe_heading',   'First Dibs on Seasonal Bouquets — Every Sunday.', 'text');

-- +goose Down
DROP TABLE IF EXISTS site_content;
DROP TABLE IF EXISTS branch_product_rules;
DROP TABLE IF EXISTS modifier_options;
DROP TABLE IF EXISTS modifier_groups;
DROP TABLE IF EXISTS product_variants;
DROP TABLE IF EXISTS product_images;
DROP TABLE IF EXISTS products;
DROP TABLE IF EXISTS product_categories;
