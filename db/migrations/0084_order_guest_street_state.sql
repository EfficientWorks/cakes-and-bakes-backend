-- +goose Up
-- +goose StatementBegin

/*
 * The street and state typed at the till, kept on the order.
 *
 * A POS order already keeps the address, city and postcode it was rung up
 * with (guest_customer_*), so a later change to the customer's address does
 * not rewrite an old order. Street and state lived only on the customer's
 * address record, so an old order could show a newer street beside its own
 * city. They are kept here too now; the customer record still gets them.
 */
ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS guest_customer_street TEXT,
    ADD COLUMN IF NOT EXISTS guest_customer_state  TEXT;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE orders
    DROP COLUMN IF EXISTS guest_customer_state,
    DROP COLUMN IF EXISTS guest_customer_street;
-- +goose StatementEnd
