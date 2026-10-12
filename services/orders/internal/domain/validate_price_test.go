package domain

import (
	"testing"

	"bakehouse/services/orders/ordersapi"
)

// The two sides of the price comparison have to mean the same thing.
//
// GetPricedItem returns a unit price with the chosen modifiers folded in; a
// cart line stores the base price with its modifiers beside it. Comparing the
// first against the second reported a price change on every line carrying any
// modifier — a $82 bouquet was announced as having risen to $88 on the way to
// checkout, and nothing had moved.
func TestUnitTotalIsWhatARepricedItemShouldMatch(t *testing.T) {
	item := ordersapi.CartItem{
		UnitPriceCents:   7600, // base
		SelectedFlavours: []ordersapi.SelectedModifier{{PriceDelta: 0}},
		SelectedPresentation: &ordersapi.SelectedModifier{
			ID: "size-6", Name: "6 Cupcakes", PriceDelta: 600,
		},
	}

	if got := item.UnitTotalCents(); got != 8200 {
		t.Fatalf("UnitTotalCents = %d, want 8200 (7600 base + 600 size)", got)
	}

	// What the catalogue would quote for the same selection: one number, with
	// the delta already inside it.
	const repriced int64 = 8200

	if repriced == item.UnitPriceCents {
		t.Fatal("the test is not exercising the bug — the base and the total are the same here")
	}
	if repriced != item.UnitTotalCents() {
		t.Errorf("a repriced item (%d) does not equal the line total (%d), so an unchanged price "+
			"would be reported as changed", repriced, item.UnitTotalCents())
	}
}

// A genuine change must still be caught.
func TestAGenuinePriceRiseIsStillDetected(t *testing.T) {
	item := ordersapi.CartItem{
		UnitPriceCents:       7600,
		SelectedPresentation: &ordersapi.SelectedModifier{PriceDelta: 600},
	}
	const repricedAfterARise int64 = 9000

	if repricedAfterARise == item.UnitTotalCents() {
		t.Error("a real rise from 8200 to 9000 was treated as no change")
	}
}

func TestBranchModifierOverrideInUnitTotal(t *testing.T) {
	// Base price: 7600.
	// Base modifier delta: 500.
	itemWithBaseModifier := ordersapi.CartItem{
		UnitPriceCents: 7600,
		SelectedFlavours: []ordersapi.SelectedModifier{
			{ID: "mod-1", Name: "Chocolate", PriceDelta: 500},
		},
	}
	if got := itemWithBaseModifier.UnitTotalCents(); got != 8100 {
		t.Fatalf("itemWithBaseModifier.UnitTotalCents() = %d, want 8100", got)
	}

	// When branch has a modifier rule with price override 800:
	itemWithBranchModifierOverride := ordersapi.CartItem{
		UnitPriceCents: 7600,
		SelectedFlavours: []ordersapi.SelectedModifier{
			{ID: "mod-1", Name: "Chocolate", PriceDelta: 800},
		},
	}
	if got := itemWithBranchModifierOverride.UnitTotalCents(); got != 8400 {
		t.Fatalf("itemWithBranchModifierOverride.UnitTotalCents() = %d, want 8400", got)
	}
}
