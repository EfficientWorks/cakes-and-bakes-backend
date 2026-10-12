-- +goose Up

-- A "new customer" coupon: one code, live at a time, that a signed-in
-- customer may use on their first order only. It has no date window (it runs
-- until it is paused or replaced) and, because `type` is what the other
-- coupon kinds use to say how they discount, it carries its own discount_kind
-- — any of the other types — to say what it gives.
ALTER TABLE coupons DROP CONSTRAINT IF EXISTS coupons_type_check;
ALTER TABLE coupons
    ADD CONSTRAINT coupons_type_check
        CHECK (type IN ('percentage','fixed','free_shipping','free_delivery','new_customer'));

ALTER TABLE coupons
    ADD COLUMN IF NOT EXISTS discount_kind TEXT NOT NULL DEFAULT 'percentage'
        CHECK (discount_kind IN ('percentage','fixed','free_shipping','free_delivery')),
    ADD COLUMN IF NOT EXISTS show_on_checkout BOOLEAN NOT NULL DEFAULT false;

-- Only one new-customer coupon may be active at once. The service checks
-- first and explains; the index is what makes two concurrent saves unable to
-- both win.
CREATE UNIQUE INDEX IF NOT EXISTS idx_coupons_one_active_new_customer
    ON coupons ((true))
    WHERE type = 'new_customer' AND status = 'active' AND deleted_at IS NULL;

-- Redemptions were counted but never recorded per customer, which is what a
-- once-per-customer coupon needs to read back.
CREATE INDEX IF NOT EXISTS idx_coupon_redemptions_customer
    ON coupon_redemptions (customer_id, coupon_id)
    WHERE customer_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_coupon_redemptions_customer;
DROP INDEX IF EXISTS idx_coupons_one_active_new_customer;
ALTER TABLE coupons
    DROP COLUMN IF EXISTS show_on_checkout,
    DROP COLUMN IF EXISTS discount_kind;
UPDATE coupons SET type = 'percentage' WHERE type = 'new_customer';
ALTER TABLE coupons DROP CONSTRAINT IF EXISTS coupons_type_check;
ALTER TABLE coupons
    ADD CONSTRAINT coupons_type_check
        CHECK (type IN ('percentage','fixed','free_shipping','free_delivery'));
