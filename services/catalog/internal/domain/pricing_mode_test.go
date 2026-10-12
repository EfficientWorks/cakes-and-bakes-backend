package domain

import (
	"testing"

	"bakehouse/services/catalog/catalogapi"
)

func TestValidateProduct_PricingMode(t *testing.T) {
	base := catalogapi.Product{Name: "A", Slug: "a", CategoryID: "c"}

	bad := base
	bad.BasePriceCents = 100
	bad.PricingMode = "bogus"
	if err := validateProduct(bad); err == nil {
		t.Fatal("an unknown pricing mode must be rejected")
	}

	// Product-priced: the base price is what is charged, so it must be set.
	zeroBase := base
	if err := validateProduct(zeroBase); err == nil {
		t.Fatal("product-priced item with no base price must be rejected")
	}

	// Variation-priced: the options are the price, so a zero base is fine.
	variation := base
	variation.PricingMode = catalogapi.PricingModeVariation
	if err := validateProduct(variation); err != nil {
		t.Fatalf("variation-priced item with zero base price must pass, got %v", err)
	}

	negative := variation
	negative.BasePriceCents = -1
	if err := validateProduct(negative); err == nil {
		t.Fatal("a negative base price must be rejected in every mode")
	}
}
