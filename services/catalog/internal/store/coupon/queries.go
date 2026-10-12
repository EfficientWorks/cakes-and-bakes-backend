package coupon

const couponColumns = `id, code, name, type, discount_value, status, applies_to, target_ids,
	       usage_limit, redeemed_count, has_active_dates, starts_at, ends_at,
	       has_minimum_order, minimum_order_cents, branch_id, show_on_checkout, created_at`

const couponSelect = `
	SELECT ` + couponColumns + `
	FROM coupons`
