-- +goose Up

CREATE TABLE orders (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reference       TEXT UNIQUE NOT NULL,
    franchise_id    UUID NOT NULL REFERENCES franchises(id),
    branch_id       UUID NOT NULL REFERENCES branches(id),
    customer_id     UUID REFERENCES customers(id),
    channel         TEXT NOT NULL DEFAULT 'web' CHECK (channel IN ('web','pos','phone','kds_manual')),
    fulfilment_type TEXT NOT NULL CHECK (fulfilment_type IN ('pickup','delivery')),

    -- Recipient — shown first and prominently throughout (SF-22, CX-01).
    recipient_first_name TEXT NOT NULL,
    recipient_last_name  TEXT NOT NULL,
    recipient_phone      TEXT,
    gift_message          TEXT,
    is_gift               BOOLEAN NOT NULL DEFAULT true,

    -- Purchaser
    purchaser_first_name TEXT NOT NULL,
    purchaser_last_name  TEXT NOT NULL,
    purchaser_email      TEXT NOT NULL,
    purchaser_phone      TEXT,

    -- Delivery address (null for pickup)
    delivery_street       TEXT,
    delivery_city         TEXT,
    delivery_state        TEXT,
    delivery_postcode     TEXT,
    delivery_instructions TEXT,

    -- Scheduling
    scheduled_date   DATE NOT NULL,
    slot_start       TIME NOT NULL,
    slot_end         TIME NOT NULL,
    slot_capacity_id UUID REFERENCES slot_capacity(id),

    -- Financials, all in cents (NFR: integer minor units)
    subtotal_cents     INTEGER NOT NULL,
    delivery_fee_cents INTEGER NOT NULL DEFAULT 0,
    tax_cents          INTEGER NOT NULL DEFAULT 0,
    tip_cents          INTEGER NOT NULL DEFAULT 0,
    discount_cents     INTEGER NOT NULL DEFAULT 0,
    total_cents        INTEGER NOT NULL,
    currency           TEXT NOT NULL DEFAULT 'USD',

    status TEXT NOT NULL DEFAULT 'placed' CHECK (status IN
        ('placed','confirmed','scheduled','in_production','ready','out_for_delivery','fulfilled','cancelled')),

    payment_status            TEXT NOT NULL DEFAULT 'pending' CHECK (payment_status IN
        ('pending','captured','failed','refunded','partial_refund')),
    stripe_payment_intent_id  TEXT,

    idempotency_key   TEXT UNIQUE NOT NULL,
    marketing_opt_in  BOOLEAN NOT NULL DEFAULT false,
    notes             TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_orders_branch ON orders (branch_id, scheduled_date);
CREATE INDEX idx_orders_franchise ON orders (franchise_id);
CREATE INDEX idx_orders_customer ON orders (customer_id) WHERE customer_id IS NOT NULL;
CREATE INDEX idx_orders_status ON orders (status);
CREATE INDEX idx_orders_reference ON orders (reference);

CREATE TABLE order_items (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id          UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    product_id        UUID NOT NULL REFERENCES products(id),
    product_name      TEXT NOT NULL, -- denormalised: history must survive a catalogue edit
    variant_id        UUID REFERENCES product_variants(id),
    variant_name      TEXT,
    quantity          INTEGER NOT NULL DEFAULT 1,
    unit_price_cents  INTEGER NOT NULL,
    line_total_cents  INTEGER NOT NULL,

    selected_flavours     JSONB NOT NULL DEFAULT '[]',
    selected_addons       JSONB NOT NULL DEFAULT '[]',
    selected_presentation JSONB,
    selected_wrap_colour  JSONB,

    production_status     TEXT NOT NULL DEFAULT 'pending' CHECK (production_status IN ('pending','in_progress','done')),
    production_started_at TIMESTAMPTZ,
    production_done_at    TIMESTAMPTZ,
    production_by_user_id UUID
);

CREATE INDEX idx_order_items_order ON order_items (order_id);

CREATE TABLE order_amendments (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id          UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    actor_id          UUID,
    actor_role        TEXT NOT NULL,
    reason            TEXT NOT NULL,
    before_value      JSONB NOT NULL,
    after_value       JSONB NOT NULL,
    price_delta_cents INTEGER NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_order_amendments_order ON order_amendments (order_id);

CREATE TABLE order_status_events (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id   UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    status     TEXT NOT NULL,
    actor_id   UUID,
    actor_type TEXT NOT NULL DEFAULT 'system' CHECK (actor_type IN ('customer','staff','system')),
    note       TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_order_status_events_order ON order_status_events (order_id, created_at);

-- +goose Down
DROP TABLE IF EXISTS order_status_events;
DROP TABLE IF EXISTS order_amendments;
DROP TABLE IF EXISTS order_items;
DROP TABLE IF EXISTS orders;
