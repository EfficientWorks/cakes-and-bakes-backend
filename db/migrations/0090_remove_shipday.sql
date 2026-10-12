-- +goose Up
-- +goose StatementBegin

/*
 * Shipday courier dispatch is removed from the product (0077, 0078).
 *
 * Delivery orders are taken out by the store's own staff, who move them
 * through ready -> out_for_delivery -> fulfilled on the POS, KDS or admin.
 * Order responses keep their "delivery" field as null for the POS contract.
 */
DROP TABLE IF EXISTS order_deliveries;
ALTER TABLE branches DROP COLUMN IF EXISTS shipday_enabled;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE branches ADD COLUMN IF NOT EXISTS shipday_enabled BOOLEAN NOT NULL DEFAULT false;

CREATE TABLE IF NOT EXISTS order_deliveries (
    order_id        UUID PRIMARY KEY REFERENCES orders(id) ON DELETE CASCADE,
    provider        TEXT NOT NULL DEFAULT 'shipday',
    external_id     TEXT,
    status          TEXT NOT NULL CHECK (status IN (
                        'dispatching','dispatched','assigned','picked_up','on_the_way',
                        'delivered','failed','cancelled','error')),
    provider_status TEXT,
    last_event      TEXT,
    last_event_at   TIMESTAMPTZ,
    tracking_url    TEXT,
    driver_name     TEXT,
    driver_phone    TEXT,
    expected_at     TIMESTAMPTZ,
    last_error      TEXT,
    attempts        INTEGER NOT NULL DEFAULT 0,
    dispatched_at   TIMESTAMPTZ,
    picked_up_at    TIMESTAMPTZ,
    delivered_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_order_deliveries_external
    ON order_deliveries (provider, external_id) WHERE external_id IS NOT NULL;

-- +goose StatementEnd
