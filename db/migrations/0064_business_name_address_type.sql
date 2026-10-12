-- +goose Up

-- Website checkouts can deliver to a workplace, and a courier needs both the
-- company on the door and to know whether the address is a home or a business
-- (reception hours, loading bays). Kept on the order so the run sheet reads
-- what was ordered, and on the recipient book so the next order pre-fills it.
ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS business_name TEXT,
    ADD COLUMN IF NOT EXISTS delivery_address_type TEXT;

ALTER TABLE orders
    DROP CONSTRAINT IF EXISTS orders_delivery_address_type_check;
ALTER TABLE orders
    ADD CONSTRAINT orders_delivery_address_type_check
        CHECK (delivery_address_type IS NULL OR delivery_address_type IN ('residence', 'business'));

-- A purchaser may be a guest (identified only by email) or a signed-in
-- customer. customer_id is nullable for the guest case and lets a customer's
-- recipient book follow them if they ever change their email.
ALTER TABLE storefront_recipients
    ADD COLUMN IF NOT EXISTS customer_id UUID REFERENCES customers(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS business_name TEXT,
    ADD COLUMN IF NOT EXISTS delivery_address_type TEXT;

ALTER TABLE storefront_recipients
    DROP CONSTRAINT IF EXISTS storefront_recipients_delivery_address_type_check;
ALTER TABLE storefront_recipients
    ADD CONSTRAINT storefront_recipients_delivery_address_type_check
        CHECK (delivery_address_type IS NULL OR delivery_address_type IN ('residence', 'business'));

CREATE INDEX IF NOT EXISTS idx_storefront_recipients_customer_id
    ON storefront_recipients (customer_id)
    WHERE customer_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_storefront_recipients_customer_id;
ALTER TABLE storefront_recipients
    DROP CONSTRAINT IF EXISTS storefront_recipients_delivery_address_type_check,
    DROP COLUMN IF EXISTS delivery_address_type,
    DROP COLUMN IF EXISTS business_name,
    DROP COLUMN IF EXISTS customer_id;

ALTER TABLE orders
    DROP CONSTRAINT IF EXISTS orders_delivery_address_type_check,
    DROP COLUMN IF EXISTS delivery_address_type,
    DROP COLUMN IF EXISTS business_name;
