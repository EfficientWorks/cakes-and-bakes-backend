-- +goose Up
-- +goose StatementBegin

/*
 * Third-party delivery dispatch (Shipday).
 *
 * One row per delivery order handed to a courier platform. Kept beside the
 * order rather than on it: the order row is read by every screen, while this
 * is written by a webhook several times per delivery and read by a few.
 *
 *   dispatching -> dispatched -> assigned -> picked_up -> on_the_way -> delivered
 *                                        \-> failed      (any) -> cancelled
 *   error: the provider refused the order or could not be reached; staff
 *          re-dispatch from the admin order page.
 */
CREATE TABLE IF NOT EXISTS order_deliveries (
    order_id        UUID PRIMARY KEY REFERENCES orders(id) ON DELETE CASCADE,
    provider        TEXT NOT NULL DEFAULT 'shipday',
    external_id     TEXT,
    status          TEXT NOT NULL CHECK (status IN (
                        'dispatching','dispatched','assigned','picked_up','on_the_way',
                        'delivered','failed','cancelled','error')),
    provider_status TEXT,
    last_event      TEXT,
    -- When the provider says the last applied event happened; older events
    -- (webhooks arrive out of order) are recorded but do not move the status.
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

-- The webhook finds the delivery by the provider's order id.
CREATE UNIQUE INDEX IF NOT EXISTS idx_order_deliveries_external
    ON order_deliveries (provider, external_id) WHERE external_id IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS order_deliveries;
