package api

import (
	"encoding/json"
	"testing"

	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/catalog/internal/domain"
)

func variationPricedProduct() domain.ProductWithAvailability {
	group := func(name string, prices ...int64) catalogapi.ModifierGroup {
		g := catalogapi.ModifierGroup{ID: name, Name: name, Kind: catalogapi.ModifierKindVariation, Type: catalogapi.ModifierFlavour}
		for i, p := range prices {
			g.Options = append(g.Options, catalogapi.ModifierOption{ID: name + string(rune('a'+i)), Name: name, PriceDeltaCents: p})
		}
		return g
	}
	return domain.ProductWithAvailability{
		Product: catalogapi.Product{
			ID: "p1", Name: "Product A", Slug: "product-a", BasePriceCents: 100,
			PricingMode: catalogapi.PricingModeVariation, IsActive: true,
			ModifierGroups: []catalogapi.ModifierGroup{group("size", 10, 30), group("box", 5, 10)},
		},
		Availability: catalogapi.Availability{Available: true},
	}
}

func roundTrip(t *testing.T, v interface{}) map[string]interface{} {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func TestProductListDTO_VariationPricedCarriesGroupsAndRange(t *testing.T) {
	p := variationPricedProduct()
	got := roundTrip(t, productListDTO(&p))

	if got["pricingMode"] != "variation" {
		t.Fatalf("pricingMode = %v, want variation", got["pricingMode"])
	}
	groups, _ := got["modifierGroups"].([]interface{})
	if len(groups) != 2 {
		t.Fatalf("website list card must carry the 2 variation groups, got %v", got["modifierGroups"])
	}
	if got["minPriceCents"] != float64(15) || got["maxPriceCents"] != float64(40) {
		t.Fatalf("range = %v–%v, want 15–40", got["minPriceCents"], got["maxPriceCents"])
	}

	// The POS list shape gets the same, in its own group encoding.
	pos := roundTrip(t, posProductListDTO(&p))
	if pos["pricingMode"] != "variation" {
		t.Fatalf("pos pricingMode = %v", pos["pricingMode"])
	}
	if groups, _ := pos["modifierGroups"].([]interface{}); len(groups) != 2 {
		t.Fatalf("POS list card must carry the 2 variation groups, got %v", pos["modifierGroups"])
	}
}

func TestProductListDTO_ProductPricedStaysLight(t *testing.T) {
	p := variationPricedProduct()
	p.PricingMode = catalogapi.PricingModeProduct
	got := roundTrip(t, productListDTO(&p))

	if got["pricingMode"] != "product" {
		t.Fatalf("pricingMode = %v, want product", got["pricingMode"])
	}
	if _, present := got["modifierGroups"]; present {
		t.Fatal("product-priced list card must not carry modifier groups")
	}
	// No branch overrides → the existing behaviour: no range on the card.
	if got["minPriceCents"] != nil || got["maxPriceCents"] != nil {
		t.Fatalf("unexpected range on product-priced card: %v–%v", got["minPriceCents"], got["maxPriceCents"])
	}
}

func TestProductDetailDTO_PriceRange(t *testing.T) {
	p := variationPricedProduct()
	got := roundTrip(t, productDetailDTO(&p, nil))
	if got["minPriceCents"] != float64(15) || got["maxPriceCents"] != float64(40) {
		t.Fatalf("variation-priced detail range = %v–%v, want 15–40", got["minPriceCents"], got["maxPriceCents"])
	}
	if groups, _ := got["modifierGroups"].([]interface{}); len(groups) != 2 {
		t.Fatalf("detail must list the groups once, got %v", got["modifierGroups"])
	}

	p.PricingMode = catalogapi.PricingModeProduct
	got = roundTrip(t, productDetailDTO(&p, nil))
	if got["minPriceCents"] != float64(115) || got["maxPriceCents"] != float64(140) {
		t.Fatalf("product-priced detail range = %v–%v, want 115–140", got["minPriceCents"], got["maxPriceCents"])
	}

	p.ModifierGroups = nil
	got = roundTrip(t, productDetailDTO(&p, nil))
	if got["minPriceCents"] != float64(100) || got["maxPriceCents"] != float64(100) {
		t.Fatalf("no-variation detail range = %v–%v, want 100–100", got["minPriceCents"], got["maxPriceCents"])
	}
}

func TestProductRequest_PricingModeRoundTrip(t *testing.T) {
	var req productRequest
	if err := json.Unmarshal([]byte(`{"name":"A","pricingMode":"variation"}`), &req); err != nil {
		t.Fatal(err)
	}
	if got := productFromRequest(req).PricingMode; got != catalogapi.PricingModeVariation {
		t.Fatalf("create body: got %q", got)
	}
	if got := productFromRequest(productRequest{}).PricingMode; got != catalogapi.PricingModeProduct {
		t.Fatalf("create body without the key must default to product, got %q", got)
	}

	var patchReq productPatchRequest
	if err := json.Unmarshal([]byte(`{"pricingMode":"variation"}`), &patchReq); err != nil {
		t.Fatal(err)
	}
	patch, err := productPatchFromRequest(patchReq)
	if err != nil {
		t.Fatal(err)
	}
	if patch.PricingMode == nil || *patch.PricingMode != catalogapi.PricingModeVariation {
		t.Fatalf("patch body: got %v", patch.PricingMode)
	}
	if got := adminProductDTO(&catalogapi.Product{PricingMode: catalogapi.PricingModeVariation}).PricingMode; got != "variation" {
		t.Fatalf("admin DTO: got %q", got)
	}
}
