package domain

import (
	"context"
	"errors"
	"time"

	"bakehouse/pkg/apperr"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/catalog/internal/store"
)

// QuoteCoupon prices a discount code against a described cart.
//
// Unauthenticated on purpose: this is what a shopper's "apply code" button
// calls, and guest checkout has no session to check. Nothing is written and
// nothing is disclosed beyond whether this particular basket qualifies — a
// wrong code and a paused code are both simply "that code is not valid",
// while the conditions a shopper can act on (a minimum, a location) say so.
func (s *Service) QuoteCoupon(ctx context.Context, q catalogapi.CouponQuote) (*catalogapi.CouponDiscount, error) {
	code := catalogapi.NormaliseCouponCode(q.Code)
	if code == "" {
		return nil, apperr.Validation("Enter a discount code.")
	}

	coupon, err := s.store.GetCouponByCode(ctx, code)
	if errors.Is(err, store.ErrNotFound) {
		return nil, apperr.New("COUPON_INVALID", apperr.CategoryValidation, "That code is not valid.")
	}
	if err != nil {
		return nil, apperr.Internal(err)
	}

	if coupon.IsNewCustomer() && q.CustomerID != nil && *q.CustomerID != "" {
		isNew, err := s.store.CustomerIsNew(ctx, *q.CustomerID, coupon.ID)
		if err != nil {
			return nil, apperr.Internal(err)
		}
		q.IsNewCustomer = isNew
	}

	discount, err := coupon.Quote(q, time.Now())
	if err != nil {
		return nil, couponRejection(err)
	}
	return discount, nil
}

// StorefrontCoupon is a coupon as the public list shows it, with whether the
// caller can use it worked out.
type StorefrontCoupon struct {
	catalogapi.Coupon
	Eligible         bool
	IneligibleReason string
}

// ListStorefrontCoupons is the public list of live codes. customerID is the
// signed-in customer, nil for a guest; it decides whether a new-customer
// code is marked usable.
func (s *Service) ListStorefrontCoupons(ctx context.Context, f store.StorefrontCouponFilter, customerID *string) ([]StorefrontCoupon, error) {
	coupons, err := s.store.ListStorefrontCoupons(ctx, f)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	out := make([]StorefrontCoupon, 0, len(coupons))
	for _, c := range coupons {
		sc := StorefrontCoupon{Coupon: c, Eligible: true}
		if c.IsNewCustomer() {
			switch {
			case customerID == nil || *customerID == "":
				sc.Eligible, sc.IneligibleReason = false, "login_required"
			default:
				isNew, err := s.store.CustomerIsNew(ctx, *customerID, c.ID)
				if err != nil {
					return nil, apperr.Internal(err)
				}
				if !isNew {
					sc.Eligible, sc.IneligibleReason = false, "not_new_customer"
				}
			}
		}
		out = append(out, sc)
	}
	return out, nil
}

// RedeemCoupon records one use of a code, at the moment an order is created.
//
// Separate from QuoteCoupon because a shopper may price a code many times
// while editing their basket and redeem it once. Called by the orders module
// through the CouponPricer contract.
func (s *Service) RedeemCoupon(ctx context.Context, couponID, orderID string, customerID *string, amountCents int64) error {
	if err := s.store.RecordCouponRedemption(ctx, couponID, orderID, customerID, amountCents); err != nil {
		if errors.Is(err, catalogapi.ErrCouponUsedUp) {
			return couponRejection(err)
		}
		return apperr.Internal(err)
	}
	return nil
}

// couponRejection turns a rule the coupon failed into something worth reading.
//
// Each reason gets its own sentence: a shopper told "invalid code" when the
// real problem is a $50 minimum has no idea they are two macarons away from
// the discount, and abandons the basket.
func couponRejection(err error) error {
	switch {
	case errors.Is(err, catalogapi.ErrCouponBelowMinimum):
		return apperr.New("COUPON_BELOW_MINIMUM", apperr.CategoryValidation,
			"Your order is below the minimum for this code.")
	case errors.Is(err, catalogapi.ErrCouponUsedUp):
		return apperr.New("COUPON_USED_UP", apperr.CategoryValidation,
			"This code has reached its usage limit.")
	case errors.Is(err, catalogapi.ErrCouponNotStarted):
		return apperr.New("COUPON_NOT_STARTED", apperr.CategoryValidation,
			"This code is not active yet.")
	case errors.Is(err, catalogapi.ErrCouponExpired):
		return apperr.New("COUPON_EXPIRED", apperr.CategoryValidation, "This code has expired.")
	case errors.Is(err, catalogapi.ErrCouponWrongBranch):
		return apperr.New("COUPON_WRONG_BRANCH", apperr.CategoryValidation,
			"This code is not valid at the location fulfilling your order.")
	case errors.Is(err, catalogapi.ErrCouponNoMatchItems):
		return apperr.New("COUPON_NO_MATCHING_ITEMS", apperr.CategoryValidation,
			"Nothing in your basket qualifies for this code.")
	case errors.Is(err, catalogapi.ErrCouponLoginRequired):
		return apperr.New("COUPON_LOGIN_REQUIRED", apperr.CategoryValidation,
			"This code is for new customers. Sign in or create an account to use it.")
	case errors.Is(err, catalogapi.ErrCouponNotNewCustomer):
		return apperr.New("COUPON_NOT_NEW_CUSTOMER", apperr.CategoryValidation,
			"This code is for first orders only and cannot be used on this account.")
	default:
		// A paused or unknown-type coupon says as little as a wrong code
		// does — there is nothing useful a shopper can do with the detail,
		// and it would confirm the code exists.
		return apperr.New("COUPON_INVALID", apperr.CategoryValidation, "That code is not valid.")
	}
}

// CategoryIDsForProducts maps product ids onto the categories they belong to,
// so a coupon restricted by category can be judged against a basket that only
// knows its products.
func (s *Service) CategoryIDsForProducts(ctx context.Context, productIDs []string) ([]string, error) {
	if len(productIDs) == 0 {
		return nil, nil
	}
	ids, err := s.store.CategoryIDsForProducts(ctx, productIDs)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return ids, nil
}
