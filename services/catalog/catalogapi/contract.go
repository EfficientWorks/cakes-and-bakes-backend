// Package catalogapi is the catalogue service's published contract: the
// domain types it owns and the interfaces other services consume. It holds
// no logic and imports nothing from the service itself.
package catalogapi

import (
	"context"
	"time"
)

// Pricer resolves and prices one cart line from customer-selected ids,
// confirming every id exists, is active, and belongs to the right
// product/group. Orders depends only on this — it never computes a price
// itself and never trusts one supplied by a client.
type Pricer interface {
	GetPricedItem(
		ctx context.Context,
		branchID, productID string,
		variantID *string,
		flavourIDs, addonIDs []string,
		presentationID, wrapColourID *string,
		date time.Time,
	) (*PricedItem, error)
}

// ProductSale is one line of a completed order, reduced to the only two
// things the popularity rollup cares about.
type ProductSale struct {
	ProductID string
	Quantity  int
}

// SalesRecorder feeds `products.popularity_score` from real order volume.
//
// That column is what the "Most Gifted" sort ranks by, and
// 0017_catalog_discovery.sql left it to "a rollup outside the catalogue
// service" that was never built — so every product sat at 0 and the sort
// silently degraded to the admin's hand-set is_best_seller and sort_order.
// This interface is that rollup: orders calls it once, when an order is
// marked fulfilled.
//
// Fulfilment rather than placement is the trigger on purpose. An order that
// is placed and then cancelled never reached anybody, and counting it would
// let a charge-then-refund push a product up the ranking.
//
// Fire-and-forget: a ranking that is briefly stale is not worth failing a
// staff member's "mark as delivered" over, so orders logs a failure here and
// carries on.
type SalesRecorder interface {
	RecordProductSales(ctx context.Context, sales []ProductSale) error
}

// CouponPricer is how orders reaches the discount rules without knowing them.
//
// Two calls, deliberately separate: a shopper may price a code many times
// while editing their basket, and redeem it exactly once when the order is
// created. Folding them together would either count every keystroke as a
// redemption or leave the usage limit unenforced.
type CouponPricer interface {
	// QuoteCoupon prices a code against a described cart, or explains why it
	// does not apply.
	QuoteCoupon(ctx context.Context, q CouponQuote) (*CouponDiscount, error)
	// RedeemCoupon records one use against the order that used it. Returns
	// an error if the limit was reached between quoting and redeeming.
	RedeemCoupon(ctx context.Context, couponID, orderID string, customerID *string, amountCents int64) error
	// CategoryIDsForProducts lets a category-restricted coupon be judged
	// against a basket that only knows its product ids.
	CategoryIDsForProducts(ctx context.Context, productIDs []string) ([]string, error)
}
