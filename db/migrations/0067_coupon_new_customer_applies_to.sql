-- +goose Up

-- "New customer" is who a coupon applies to, not what it gives.
--
-- 0066 modelled it as a coupon type with a separate discount_kind. The
-- console models it the way an operator thinks of it: Coupon Type stays
-- Fixed / Percentage / Free delivery, and Applied To says All / Products /
-- Categories / New Customer. So the flag moves onto applies_to, type goes
-- back to being only the discount rule, and discount_kind is retired.
--
-- The applies_to CHECK from 0022 listed 'category'/'product' while the code
-- has always written 'categories'/'products' — a restricted coupon could not
-- be saved at all. Both spellings are accepted from here.
ALTER TABLE coupons DROP CONSTRAINT IF EXISTS coupons_applies_to_check;
ALTER TABLE coupons
    ADD CONSTRAINT coupons_applies_to_check
        CHECK (applies_to IN ('all','category','product','categories','products','new_customer'));

-- Rows saved under the 0066 model: the discount rule was in discount_kind.
UPDATE coupons
   SET applies_to = 'new_customer',
       type       = COALESCE(NULLIF(discount_kind, ''), 'percentage')
 WHERE type = 'new_customer';

ALTER TABLE coupons DROP CONSTRAINT IF EXISTS coupons_type_check;
ALTER TABLE coupons
    ADD CONSTRAINT coupons_type_check
        CHECK (type IN ('percentage','fixed','free_shipping','free_delivery'));

DROP INDEX IF EXISTS idx_coupons_one_active_new_customer;
CREATE UNIQUE INDEX IF NOT EXISTS idx_coupons_one_active_new_customer
    ON coupons ((true))
    WHERE applies_to = 'new_customer' AND status = 'active' AND deleted_at IS NULL;

ALTER TABLE coupons DROP COLUMN IF EXISTS discount_kind;

-- +goose Down
ALTER TABLE coupons
    ADD COLUMN IF NOT EXISTS discount_kind TEXT NOT NULL DEFAULT 'percentage'
        CHECK (discount_kind IN ('percentage','fixed','free_shipping','free_delivery'));
DROP INDEX IF EXISTS idx_coupons_one_active_new_customer;
ALTER TABLE coupons DROP CONSTRAINT IF EXISTS coupons_type_check;
ALTER TABLE coupons
    ADD CONSTRAINT coupons_type_check
        CHECK (type IN ('percentage','fixed','free_shipping','free_delivery','new_customer'));
UPDATE coupons
   SET discount_kind = type, type = 'new_customer', applies_to = 'all'
 WHERE applies_to = 'new_customer';
CREATE UNIQUE INDEX IF NOT EXISTS idx_coupons_one_active_new_customer
    ON coupons ((true))
    WHERE type = 'new_customer' AND status = 'active' AND deleted_at IS NULL;
ALTER TABLE coupons DROP CONSTRAINT IF EXISTS coupons_applies_to_check;
ALTER TABLE coupons
    ADD CONSTRAINT coupons_applies_to_check
        CHECK (applies_to IN ('all','category','product','categories','products'));
