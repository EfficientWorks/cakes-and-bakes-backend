package catalogapi

import (
	"errors"
	"testing"
	"time"
)

func newCustomerCoupon(kind string, value int64) Coupon {
	return Coupon{
		ID: "nc", Code: "WELCOME", Name: "First order", Type: kind,
		DiscountValue: value, Status: CouponActive, AppliesTo: CouponAppliesNewCustomer,
	}
}

// A guest cannot use a first-order code: there is no account to be new.
func TestNewCustomerCouponNeedsASignedInCustomer(t *testing.T) {
	_, err := newCustomerCoupon(CouponPercentage, 15).Quote(basket, time.Now())
	if !errors.Is(err, ErrCouponLoginRequired) {
		t.Fatalf("err = %v, want ErrCouponLoginRequired", err)
	}
}

// A signed-in customer who has ordered before is not a new customer.
func TestNewCustomerCouponRefusesReturningCustomer(t *testing.T) {
	cust := "cust-1"
	q := basket
	q.CustomerID, q.IsNewCustomer = &cust, false
	_, err := newCustomerCoupon(CouponPercentage, 15).Quote(q, time.Now())
	if !errors.Is(err, ErrCouponNotNewCustomer) {
		t.Fatalf("err = %v, want ErrCouponNotNewCustomer", err)
	}
}

// A new customer gets whichever discount kind the coupon carries.
func TestNewCustomerCouponPricesByKind(t *testing.T) {
	cust := "cust-1"
	q := basket
	q.CustomerID, q.IsNewCustomer = &cust, true

	pct, err := newCustomerCoupon(CouponPercentage, 15).Quote(q, time.Now())
	if err != nil {
		t.Fatalf("percentage: %v", err)
	}
	if pct.AmountCents != 1500 {
		t.Errorf("percentage discount = %d, want 1500", pct.AmountCents)
	}

	fixed, err := newCustomerCoupon(CouponFixed, 2500).Quote(q, time.Now())
	if err != nil {
		t.Fatalf("fixed: %v", err)
	}
	if fixed.AmountCents != 2500 {
		t.Errorf("fixed discount = %d, want 2500", fixed.AmountCents)
	}

	free, err := newCustomerCoupon(CouponFreeDelivery, 0).Quote(q, time.Now())
	if err != nil {
		t.Fatalf("free delivery: %v", err)
	}
	if !free.FreeDelivery || free.AmountCents != 800 {
		t.Errorf("free delivery = %+v, want FreeDelivery with the 800c fee", free)
	}
}

// A new-customer coupon never has a window, so dates on the row are ignored.
func TestNewCustomerCouponIgnoresDates(t *testing.T) {
	cust := "cust-1"
	q := basket
	q.CustomerID, q.IsNewCustomer = &cust, true
	c := newCustomerCoupon(CouponPercentage, 10)
	c.HasActiveDates, c.StartsAt, c.EndsAt = false, at("2000-01-01"), at("2000-01-02")
	if _, err := c.Quote(q, time.Now()); err != nil {
		t.Fatalf("Quote: %v", err)
	}
}
