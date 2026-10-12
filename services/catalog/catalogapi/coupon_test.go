package catalogapi

import (
	"errors"
	"testing"
	"time"
)

func at(s string) *time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return &t
}

func activeCoupon() Coupon {
	return Coupon{
		ID: "c1", Code: "SAVE10", Name: "Ten off", Type: CouponPercentage,
		DiscountValue: 10, Status: CouponActive, AppliesTo: CouponAppliesAll,
	}
}

var basket = CouponQuote{BranchID: "b1", SubtotalCents: 10000, DeliveryFeeCents: 800}

func TestPercentageComesOffTheSubtotal(t *testing.T) {
	got, err := activeCoupon().Quote(basket, time.Now())
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}
	if got.AmountCents != 1000 {
		t.Errorf("discount = %d, want 1000 (10%% of $100)", got.AmountCents)
	}
	if got.FreeDelivery {
		t.Error("a percentage coupon must not zero the delivery fee")
	}
}

// A fixed coupon worth more than the basket takes it to zero. Without the cap
// the order total goes negative and the customer is owed money for shopping.
func TestFixedDiscountCannotExceedTheBasket(t *testing.T) {
	c := activeCoupon()
	c.Type = CouponFixed
	c.DiscountValue = 30000

	got, err := c.Quote(basket, time.Now())
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}
	if got.AmountCents != basket.SubtotalCents {
		t.Errorf("discount = %d, want it capped at the %d subtotal", got.AmountCents, basket.SubtotalCents)
	}
}

func TestFreeDeliveryZeroesTheFeeNotTheGoods(t *testing.T) {
	c := activeCoupon()
	c.Type = CouponFreeDelivery

	got, err := c.Quote(basket, time.Now())
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}
	if !got.FreeDelivery {
		t.Error("FreeDelivery should be set")
	}
	if got.AmountCents != basket.DeliveryFeeCents {
		t.Errorf("discount = %d, want the %d delivery fee", got.AmountCents, basket.DeliveryFeeCents)
	}
}

func TestMinimumOrderIsEnforced(t *testing.T) {
	c := activeCoupon()
	c.HasMinimumOrder = true
	c.MinimumOrderCents = 15000

	if _, err := c.Quote(basket, time.Now()); !errors.Is(err, ErrCouponBelowMinimum) {
		t.Errorf("err = %v, want ErrCouponBelowMinimum", err)
	}
}

func TestUsageLimitIsEnforced(t *testing.T) {
	c := activeCoupon()
	limit := 5
	c.UsageLimit = &limit
	c.RedeemedCount = 5

	if _, err := c.Quote(basket, time.Now()); !errors.Is(err, ErrCouponUsedUp) {
		t.Errorf("err = %v, want ErrCouponUsedUp", err)
	}
}

// "Valid until the 14th" has to include every hour of the 14th. Comparing
// against the stored midnight would expire the coupon a day early, which is
// the kind of bug that only shows up as complaints.
func TestEndDateIncludesTheWholeOfThatDay(t *testing.T) {
	c := activeCoupon()
	c.HasActiveDates = true
	c.EndsAt = at("2026-08-14")

	lateOnTheLastDay, _ := time.Parse(time.RFC3339, "2026-08-14T23:30:00Z")
	if _, err := c.Quote(basket, lateOnTheLastDay); err != nil {
		t.Errorf("still the 14th, but Quote said: %v", err)
	}

	theDayAfter, _ := time.Parse(time.RFC3339, "2026-08-16T00:30:00Z")
	if _, err := c.Quote(basket, theDayAfter); !errors.Is(err, ErrCouponExpired) {
		t.Errorf("err = %v, want ErrCouponExpired", err)
	}
}

func TestNotStartedYet(t *testing.T) {
	c := activeCoupon()
	c.HasActiveDates = true
	c.StartsAt = at("2026-12-01")

	now, _ := time.Parse(time.RFC3339, "2026-08-29T10:00:00Z")
	if _, err := c.Quote(basket, now); !errors.Is(err, ErrCouponNotStarted) {
		t.Errorf("err = %v, want ErrCouponNotStarted", err)
	}
}

func TestPausedCouponDoesNotApply(t *testing.T) {
	c := activeCoupon()
	c.Status = CouponPaused

	if _, err := c.Quote(basket, time.Now()); !errors.Is(err, ErrCouponNotActive) {
		t.Errorf("err = %v, want ErrCouponNotActive", err)
	}
}

func TestBranchScopedCouponRejectsOtherBranches(t *testing.T) {
	c := activeCoupon()
	other := "b2"
	c.BranchID = &other

	if _, err := c.Quote(basket, time.Now()); !errors.Is(err, ErrCouponWrongBranch) {
		t.Errorf("err = %v, want ErrCouponWrongBranch", err)
	}
}

func TestCategoryRestrictedCouponNeedsAMatchingItem(t *testing.T) {
	c := activeCoupon()
	c.AppliesTo = CouponAppliesCategories
	c.TargetIDs = []string{"cat-macarons"}

	wrong := basket
	wrong.CategoryIDs = []string{"cat-bouquets"}
	if _, err := c.Quote(wrong, time.Now()); !errors.Is(err, ErrCouponNoMatchItems) {
		t.Errorf("err = %v, want ErrCouponNoMatchItems", err)
	}

	right := basket
	right.CategoryIDs = []string{"cat-bouquets", "cat-macarons"}
	if _, err := c.Quote(right, time.Now()); err != nil {
		t.Errorf("a qualifying basket was rejected: %v", err)
	}
}

func TestCodesAreComparedWithoutCaseOrSpaces(t *testing.T) {
	for _, in := range []string{"save10", " SAVE10 ", "Save10"} {
		if got := NormaliseCouponCode(in); got != "SAVE10" {
			t.Errorf("NormaliseCouponCode(%q) = %q, want SAVE10", in, got)
		}
	}
}
