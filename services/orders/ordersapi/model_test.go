package ordersapi

import "testing"

func TestCartItemPricing(t *testing.T) {
	item := CartItem{
		UnitPriceCents:       6800,
		Quantity:             2,
		SelectedFlavours:     []SelectedModifier{{Name: "Gluten Free Vanilla", PriceDelta: 300}},
		SelectedAddons:       []SelectedModifier{{Name: "Birthday", PriceDelta: 300}, {Name: "Butterfly Topper", PriceDelta: 300}},
		SelectedPresentation: &SelectedModifier{Name: "Gift Box", PriceDelta: 600},
	}

	// 6800 base + 300 flavour + 300 + 300 addons + 600 presentation = 8300
	if got := item.UnitTotalCents(); got != 8300 {
		t.Fatalf("UnitTotalCents() = %d, want 8300", got)
	}
	if got := item.LineTotalCents(); got != 16600 {
		t.Fatalf("LineTotalCents() = %d, want 16600 (8300 * 2)", got)
	}
}

func TestIsAmendable(t *testing.T) {
	amendable := []string{StatusPlaced, StatusConfirmed, StatusScheduled}
	for _, s := range amendable {
		if !IsAmendable(s) {
			t.Errorf("expected %q to be amendable", s)
		}
	}

	locked := []string{StatusInProduction, StatusReady, StatusOutForDelivery, StatusFulfilled, StatusCancelled}
	for _, s := range locked {
		if IsAmendable(s) {
			t.Errorf("expected %q to NOT be amendable — production has locked the order", s)
		}
	}
}

func TestSlotCapacityRemaining(t *testing.T) {
	cases := []struct {
		name     string
		max, res int
		want     int
	}{
		{"has room", 10, 3, 7},
		{"exactly full", 10, 10, 0},
		{"over-reserved (should clamp, never negative)", 10, 12, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sc := SlotCapacity{MaxCapacity: c.max, ReservedCount: c.res}
			if got := sc.Remaining(); got != c.want {
				t.Errorf("Remaining() = %d, want %d", got, c.want)
			}
		})
	}
}
