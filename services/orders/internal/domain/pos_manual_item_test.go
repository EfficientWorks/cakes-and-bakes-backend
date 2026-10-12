package domain

import (
	"context"
	"testing"

	"bakehouse/services/orders/ordersapi"
)

func TestValidatePOSItems_ManualItemWithoutProductID(t *testing.T) {
	// Item with empty ProductID but valid ProductName should pass validation
	items := []ordersapi.POSItem{
		{
			ProductID:      "",
			ProductName:    "Candle",
			Quantity:       2,
			UnitPriceCents: 500,
			LineTotalCents: 1000,
		},
	}
	if err := validatePOSItems(items); err != nil {
		t.Fatalf("expected valid for manual item with empty ProductID and valid ProductName, got err: %v", err)
	}

	// Item with empty ProductID and empty ProductName should fail validation
	invalidItems := []ordersapi.POSItem{
		{
			ProductID:      "",
			ProductName:    "   ",
			Quantity:       1,
			UnitPriceCents: 500,
		},
	}
	if err := validatePOSItems(invalidItems); err == nil {
		t.Fatalf("expected validation error when both ProductID and ProductName are empty")
	}
}

func TestPricePOSItemsFromCatalog_ManualItem(t *testing.T) {
	svc := &Service{}
	pct := 10.0
	items := []ordersapi.POSItem{
		{
			ProductID:      "",
			ProductName:    "Birthday Candle",
			Quantity:       3,
			UnitPriceCents: 400,
			DiscountPct:    &pct,
		},
	}

	priced, err := svc.pricePOSItemsFromCatalog(context.Background(), "test-branch", items)
	if err != nil {
		t.Fatalf("pricePOSItemsFromCatalog failed: %v", err)
	}
	if len(priced) != 1 {
		t.Fatalf("expected 1 item, got %d", len(priced))
	}
	if priced[0].LineTotalCents != 1200 {
		t.Errorf("LineTotalCents = %d, want 1200", priced[0].LineTotalCents)
	}
	if priced[0].DiscountCents != 120 {
		t.Errorf("DiscountCents = %d, want 120 (10%% of 1200)", priced[0].DiscountCents)
	}
	if priced[0].ProductName != "Birthday Candle" {
		t.Errorf("ProductName = %q, want Birthday Candle", priced[0].ProductName)
	}
}
