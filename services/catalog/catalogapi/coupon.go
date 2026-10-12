package catalogapi

import (
	"errors"
	"strings"
	"time"
)

// CouponQuote is everything needed to decide whether a code applies to a cart
// and what it is worth.
//
// The cart is described rather than passed: catalog must not know what an
// order or a cart is, and orders must not know how a coupon is priced. This
// struct is the seam.
type CouponQuote struct {
	Code             string
	BranchID         string
	SubtotalCents    int64
	DeliveryFeeCents int64
	// What is in the cart, so a coupon restricted to particular products or
	// categories can tell whether any of them are present.
	ProductIDs    []string
	CategoryIDs   []string
	CustomerID    *string
	IsNewCustomer bool
}

// CouponDiscount is a resolved, priced coupon.
type CouponDiscount struct {
	CouponID string
	Code     string
	Name     string
	Type     string
	// AmountCents is what comes off the order, already capped so it can never
	// exceed what the coupon is being applied to.
	AmountCents int64
	// FreeDelivery is called out separately because the summary shows the
	// delivery fee as its own line; a free-delivery coupon has to be able to
	// zero that line rather than reduce the subtotal by the same number.
	FreeDelivery bool
}

// Coupon types.
const (
	CouponPercentage   = "percentage"
	CouponFixed        = "fixed"
	CouponFreeDelivery = "free_delivery"
	CouponFreeShipping = "free_shipping"
)

// IsNewCustomer reports whether the coupon is the first-order-only kind:
// it applies to new customers rather than to a basket. What it gives is
// its Type, the same as any other coupon.
func (c Coupon) IsNewCustomer() bool { return c.AppliesTo == CouponAppliesNewCustomer }

// Coupon statuses.
const (
	CouponActive    = "active"
	CouponPaused    = "paused"
	CouponScheduled = "scheduled"
	CouponExpired   = "expired"
)

// What a coupon can be restricted to.
const (
	CouponAppliesAll        = "all"
	CouponAppliesProducts   = "products"
	CouponAppliesCategories = "categories"
	// CouponAppliesNewCustomer: a signed-in customer's first order, the
	// whole basket. One such coupon may be active at a time.
	CouponAppliesNewCustomer = "new_customer"
)

// Reasons a code does not apply. Each is surfaced to the shopper as its own
// message, because "invalid code" when the real problem is a $50 minimum is
// the kind of thing that loses a sale.
var (
	ErrCouponNotFound       = errors.New("coupon: no such code")
	ErrCouponNotActive      = errors.New("coupon: not active")
	ErrCouponNotStarted     = errors.New("coupon: not started yet")
	ErrCouponExpired        = errors.New("coupon: expired")
	ErrCouponUsedUp         = errors.New("coupon: usage limit reached")
	ErrCouponBelowMinimum   = errors.New("coupon: order is below the minimum")
	ErrCouponWrongBranch    = errors.New("coupon: not valid at this location")
	ErrCouponNoMatchItems   = errors.New("coupon: nothing in the basket qualifies")
	ErrCouponLoginRequired  = errors.New("coupon: sign in required")
	ErrCouponNotNewCustomer = errors.New("coupon: not a new customer")
)

// NormaliseCouponCode is how codes are compared. Shoppers type them by hand,
// off a card or out of an email, so case and stray spaces cannot be the
// difference between a working code and a rejected one.
func NormaliseCouponCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

// Quote decides whether this coupon applies to the described cart and, if it
// does, what it is worth.
//
// `now` is passed in rather than read from the clock so the decision is
// testable and so a single request cannot straddle a coupon's expiry.
func (c Coupon) Quote(q CouponQuote, now time.Time) (*CouponDiscount, error) {
	if c.Status != CouponActive {
		return nil, ErrCouponNotActive
	}
	if c.HasActiveDates {
		if c.StartsAt != nil && now.Before(*c.StartsAt) {
			return nil, ErrCouponNotStarted
		}
		// End dates are stored as a date, which means the whole of that day
		// counts — a coupon "until the 14th" that stops working at midnight
		// on the 13th is a support ticket.
		if c.EndsAt != nil && now.After(c.EndsAt.Add(24*time.Hour)) {
			return nil, ErrCouponExpired
		}
	}
	if c.UsageLimit != nil && c.RedeemedCount >= *c.UsageLimit {
		return nil, ErrCouponUsedUp
	}
	if c.HasMinimumOrder && q.SubtotalCents < c.MinimumOrderCents {
		return nil, ErrCouponBelowMinimum
	}
	if c.BranchID != nil && *c.BranchID != "" && *c.BranchID != q.BranchID {
		return nil, ErrCouponWrongBranch
	}
	if c.IsNewCustomer() {
		if q.CustomerID == nil || *q.CustomerID == "" {
			return nil, ErrCouponLoginRequired
		}
		if !q.IsNewCustomer {
			return nil, ErrCouponNotNewCustomer
		}
	}

	// A restricted coupon discounts only the qualifying part of the basket,
	// not the whole of it — otherwise "20% off macarons" takes 20% off the
	// bouquet sitting beside them.
	base := q.SubtotalCents
	switch c.AppliesTo {
	case CouponAppliesProducts:
		if !anyOf(q.ProductIDs, c.TargetIDs) {
			return nil, ErrCouponNoMatchItems
		}
	case CouponAppliesCategories:
		if !anyOf(q.CategoryIDs, c.TargetIDs) {
			return nil, ErrCouponNoMatchItems
		}
	}

	out := &CouponDiscount{CouponID: c.ID, Code: c.Code, Name: c.Name, Type: c.Type}

	switch c.Type {
	case CouponFreeDelivery, CouponFreeShipping:
		out.FreeDelivery = true
		out.AmountCents = q.DeliveryFeeCents

	case CouponFixed:
		out.AmountCents = c.DiscountValue

	case CouponPercentage:
		// Integer arithmetic throughout — money never becomes a float. The
		// division truncates, so a rounding difference always favours the
		// shop by at most a cent rather than drifting either way.
		out.AmountCents = base * c.DiscountValue / 100

	default:
		return nil, ErrCouponNotActive
	}

	// Never discount more than there is to discount: a $30 fixed coupon on a
	// $20 basket takes it to zero, not to minus ten.
	if !out.FreeDelivery && out.AmountCents > base {
		out.AmountCents = base
	}
	if out.AmountCents < 0 {
		out.AmountCents = 0
	}
	return out, nil
}

func anyOf(have, want []string) bool {
	for _, h := range have {
		for _, w := range want {
			if h == w {
				return true
			}
		}
	}
	return false
}
