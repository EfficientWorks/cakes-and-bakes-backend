package coupon

import "time"

// StorefrontCouponFilter narrows the public coupon list.
type StorefrontCouponFilter struct {
	// BranchID keeps network-wide coupons plus that store's own.
	BranchID string
	// ShowOnCheckout, when set, keeps only coupons flagged for the checkout
	// page (true) or only those not (false).
	ShowOnCheckout *bool
	// NewCustomer, when set, keeps only new-customer coupons (true) or
	// everything else (false).
	NewCustomer *bool
	// Date keeps coupons whose window covers that day. A coupon with no
	// window always matches.
	Date *time.Time
}
