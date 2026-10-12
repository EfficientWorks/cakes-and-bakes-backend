package domain

import (
	"testing"

	"bakehouse/services/catalog/catalogapi"
)

func assertFold(t *testing.T, sales []catalogapi.ProductSale, wantIDs []string, wantQty []int32) {
	t.Helper()
	ids, qty := foldSales(sales)
	if len(ids) != len(wantIDs) || len(qty) != len(wantQty) {
		t.Fatalf("got %v/%v, want %v/%v", ids, qty, wantIDs, wantQty)
	}
	for i := range wantIDs {
		if ids[i] != wantIDs[i] || qty[i] != wantQty[i] {
			t.Fatalf("index %d: got %s=%d, want %s=%d", i, ids[i], qty[i], wantIDs[i], wantQty[i])
		}
	}
}

func TestFoldSales_CombinesLinesOfTheSameProduct(t *testing.T) {
	// Two lines, one product: the same bouquet ordered twice with different
	// flavours must count once, at the combined quantity.
	assertFold(t,
		[]catalogapi.ProductSale{
			{ProductID: "bouquet", Quantity: 2},
			{ProductID: "box", Quantity: 1},
			{ProductID: "bouquet", Quantity: 3},
		},
		[]string{"bouquet", "box"},
		[]int32{5, 1},
	)
}

func TestFoldSales_DropsNonPositiveQuantities(t *testing.T) {
	// A negative quantity must never reach the UPDATE — it would demote a
	// product rather than fail loudly.
	assertFold(t,
		[]catalogapi.ProductSale{
			{ProductID: "bouquet", Quantity: 0},
			{ProductID: "box", Quantity: -5},
			{ProductID: "macarons", Quantity: 4},
		},
		[]string{"macarons"},
		[]int32{4},
	)
}

func TestFoldSales_DropsBlankProductIDs(t *testing.T) {
	assertFold(t,
		[]catalogapi.ProductSale{{ProductID: "", Quantity: 9}, {ProductID: "box", Quantity: 1}},
		[]string{"box"},
		[]int32{1},
	)
}

func TestFoldSales_EmptyInputProducesNoStatement(t *testing.T) {
	// The caller skips the round-trip entirely on an empty result, so this is
	// what stops a fulfilled order with no countable lines hitting the
	// database at all.
	if ids, qty := foldSales(nil); len(ids) != 0 || len(qty) != 0 {
		t.Fatalf("expected nothing to write, got %v/%v", ids, qty)
	}
	if ids, _ := foldSales([]catalogapi.ProductSale{{ProductID: "x", Quantity: 0}}); len(ids) != 0 {
		t.Fatalf("expected nothing to write, got %v", ids)
	}
}
