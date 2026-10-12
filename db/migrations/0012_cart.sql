-- +goose Up

-- Carts are DB-backed (not Redis) for both guests and registered
-- customers: cart contents are business-critical state we don't want to
-- lose to a cache eviction, and rate-limiting/geocoding already show the
-- pattern of treating Redis as optional infrastructure — cart persistence
-- should not silently degrade. Guest carts are identified by a hashed
-- opaque token (same technique as refresh tokens) and expire after 48h.
CREATE TABLE carts (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id         UUID NOT NULL REFERENCES branches(id),
    customer_id       UUID REFERENCES customers(id),
    guest_token_hash  TEXT UNIQUE,
    fulfilment_type   TEXT NOT NULL DEFAULT 'delivery' CHECK (fulfilment_type IN ('pickup','delivery')),

    delivery_street   TEXT,
    delivery_city     TEXT,
    delivery_state    TEXT,
    delivery_postcode TEXT,

    scheduled_date    DATE,
    slot_start        TIME,
    slot_end          TIME,

    discount_code     TEXT,
    tip_cents         INTEGER NOT NULL DEFAULT 0,

    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at        TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '48 hours',

    CHECK (customer_id IS NOT NULL OR guest_token_hash IS NOT NULL)
);

CREATE INDEX idx_carts_customer ON carts (customer_id) WHERE customer_id IS NOT NULL;

CREATE TABLE cart_items (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    cart_id                UUID NOT NULL REFERENCES carts(id) ON DELETE CASCADE,
    product_id             UUID NOT NULL REFERENCES products(id),
    product_name           TEXT NOT NULL,
    variant_id             UUID REFERENCES product_variants(id),
    variant_name           TEXT,
    unit_price_cents       INTEGER NOT NULL,
    quantity               INTEGER NOT NULL DEFAULT 1 CHECK (quantity > 0),

    selected_flavours      JSONB NOT NULL DEFAULT '[]',
    selected_addons        JSONB NOT NULL DEFAULT '[]',
    selected_presentation  JSONB,
    selected_wrap_colour   JSONB,

    is_gift                BOOLEAN NOT NULL DEFAULT true,
    recipient_first_name   TEXT,
    recipient_last_name    TEXT,
    gift_message           TEXT,

    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_cart_items_cart ON cart_items (cart_id);

-- +goose Down
DROP TABLE IF EXISTS cart_items;
DROP TABLE IF EXISTS carts;
