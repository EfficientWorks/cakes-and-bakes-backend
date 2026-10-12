-- +goose Up
-- +goose StatementBegin
ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS guest_customer_name    TEXT,
    ADD COLUMN IF NOT EXISTS guest_customer_mobile  TEXT,
    ADD COLUMN IF NOT EXISTS guest_customer_address TEXT,
    ADD COLUMN IF NOT EXISTS guest_customer_email   TEXT,
    ADD COLUMN IF NOT EXISTS order_deliver_date     DATE,
    ADD COLUMN IF NOT EXISTS order_deliver_time     TIME;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE orders
    DROP COLUMN IF EXISTS order_deliver_time,
    DROP COLUMN IF EXISTS order_deliver_date,
    DROP COLUMN IF EXISTS guest_customer_email,
    DROP COLUMN IF EXISTS guest_customer_address,
    DROP COLUMN IF EXISTS guest_customer_mobile,
    DROP COLUMN IF EXISTS guest_customer_name;
-- +goose StatementEnd
