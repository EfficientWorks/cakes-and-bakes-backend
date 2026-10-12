package catalogapi

import "testing"

func variationGroup(prices ...int64) ModifierGroup {
	g := ModifierGroup{Kind: ModifierKindVariation, Type: ModifierFlavour}
	for _, p := range prices {
		g.Options = append(g.Options, ModifierOption{PriceDeltaCents: p})
	}
	return g
}

func addonGroup(prices ...int64) ModifierGroup {
	g := variationGroup(prices...)
	g.Kind = ModifierKindAddon
	g.Type = ModifierAddon
	return g
}

// The worked example from the requirement: variation sets (10, 30) and
// (5, 10) give 15–40; (10, 30) alone gives 10–30.
func TestVariationPriceRange_SumsAcrossGroups(t *testing.T) {
	p := Product{ModifierGroups: []ModifierGroup{variationGroup(10, 30), variationGroup(5, 10)}}
	min, max, has := p.VariationPriceRange()
	if !has || min != 15 || max != 40 {
		t.Fatalf("two groups: got %d–%d has=%v, want 15–40 true", min, max, has)
	}

	p = Product{ModifierGroups: []ModifierGroup{variationGroup(10, 30)}}
	min, max, has = p.VariationPriceRange()
	if !has || min != 10 || max != 30 {
		t.Fatalf("one group: got %d–%d has=%v, want 10–30 true", min, max, has)
	}
}

func TestVariationPriceRange_IgnoresAddonsAndEmptyGroups(t *testing.T) {
	p := Product{ModifierGroups: []ModifierGroup{
		variationGroup(10, 30),
		addonGroup(500, 900),
		{Kind: ModifierKindVariation},
	}}
	min, max, has := p.VariationPriceRange()
	if !has || min != 10 || max != 30 {
		t.Fatalf("got %d–%d has=%v, want 10–30 true", min, max, has)
	}

	none := Product{ModifierGroups: []ModifierGroup{addonGroup(500)}}
	if _, _, has := none.VariationPriceRange(); has {
		t.Fatal("add-on-only product must report no variations")
	}
}

func TestPriceRange_ByPricingMode(t *testing.T) {
	groups := []ModifierGroup{variationGroup(10, 30), variationGroup(5, 10)}

	variation := Product{BasePriceCents: 100, PricingMode: PricingModeVariation, ModifierGroups: groups}
	if min, max := variation.PriceRange(); min != 15 || max != 40 {
		t.Fatalf("variation-priced: got %d–%d, want 15–40", min, max)
	}

	product := Product{BasePriceCents: 100, ModifierGroups: groups}
	if min, max := product.PriceRange(); min != 115 || max != 140 {
		t.Fatalf("product-priced: got %d–%d, want 115–140", min, max)
	}

	// Flagged variation-priced but with nothing to vary: the base price is
	// the only honest answer, not zero.
	bare := Product{BasePriceCents: 100, PricingMode: PricingModeVariation}
	if min, max := bare.PriceRange(); min != 100 || max != 100 {
		t.Fatalf("variation-priced without variations: got %d–%d, want 100–100", min, max)
	}
}

func TestEffectivePricingMode_DefaultsToProduct(t *testing.T) {
	if got := (Product{}).EffectivePricingMode(); got != PricingModeProduct {
		t.Fatalf("got %q, want %q", got, PricingModeProduct)
	}
	if (Product{}).IsVariationPriced() {
		t.Fatal("empty mode must not read as variation-priced")
	}
	if !ValidPricingMode(PricingModeProduct) || !ValidPricingMode(PricingModeVariation) || ValidPricingMode("bogus") {
		t.Fatal("ValidPricingMode accepts exactly the two modes")
	}
}

func TestProductPatch_PricingMode(t *testing.T) {
	stored := Product{Name: "A", PricingMode: PricingModeProduct}

	untouched := ProductPatch{}.Apply(stored)
	if untouched.PricingMode != PricingModeProduct {
		t.Fatalf("absent field must keep the stored mode, got %q", untouched.PricingMode)
	}

	mode := PricingModeVariation
	changed := ProductPatch{PricingMode: &mode}.Apply(stored)
	if changed.PricingMode != PricingModeVariation {
		t.Fatalf("patched mode not applied, got %q", changed.PricingMode)
	}
}
