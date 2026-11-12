package catalogapi

import "testing"

func comboProduct() Product {
	size := ModifierGroup{ID: "g1", Kind: ModifierKindVariation, Options: []ModifierOption{
		{ID: "s8", Name: "8 Inch", PriceDeltaCents: 2999},
		{ID: "s10", Name: "10 Inch", PriceDeltaCents: 3999},
	}}
	sponge := ModifierGroup{ID: "g2", Kind: ModifierKindVariation, Options: []ModifierOption{
		{ID: "ve", Name: "Vanilla Eggless"},
		{ID: "rv", Name: "Red Velvet with Egg", PriceDeltaCents: 700},
	}}
	candles := ModifierGroup{ID: "g3", Kind: ModifierKindAddon, Options: []ModifierOption{{ID: "c", Name: "Candles", PriceDeltaCents: 250}}}
	typed := int64(2500)
	return Product{
		ModifierGroups: []ModifierGroup{size, sponge, candles},
		Combinations: []ProductCombination{
			{ID: "a", OptionIDs: NormaliseOptionIDs([]string{"s8", "ve"}), IsActive: true},
			{ID: "b", OptionIDs: NormaliseOptionIDs([]string{"s8", "rv"}), IsActive: false},
			{ID: "c", OptionIDs: NormaliseOptionIDs([]string{"s10", "ve"}), IsActive: true, PriceOverrideCents: &typed},
			{ID: "d", OptionIDs: NormaliseOptionIDs([]string{"s10", "gone"}), IsActive: true},
		},
	}
}

func TestCombination_PriceIsSumUnlessTyped(t *testing.T) {
	p := comboProduct()
	if got := p.CombinationPrice(p.Combinations[0]); got != 2999 {
		t.Fatalf("auto price = sum of options: want 2999, got %d", got)
	}
	if got := p.CombinationAutoPrice(p.Combinations[1]); got != 3699 {
		t.Fatalf("auto price 8in + red velvet: want 3699, got %d", got)
	}
	if got := p.CombinationPrice(p.Combinations[2]); got != 2500 {
		t.Fatalf("typed price wins: want 2500, got %d", got)
	}
}

func TestCombination_Validity(t *testing.T) {
	p := comboProduct()
	if p.CombinationValid(p.Combinations[3]) {
		t.Fatal("a row naming a removed option must be invalid")
	}
	twoSizes := ProductCombination{OptionIDs: NormaliseOptionIDs([]string{"s8", "s10"})}
	if p.CombinationValid(twoSizes) {
		t.Fatal("two options from one group must be invalid")
	}
	sizeOnly := ProductCombination{OptionIDs: []string{"s8"}}
	if p.CombinationValid(sizeOnly) {
		t.Fatal("a row missing a group must be invalid")
	}
	withAddon := ProductCombination{OptionIDs: NormaliseOptionIDs([]string{"s8", "ve", "c"})}
	if p.CombinationValid(withAddon) {
		t.Fatal("add-on options are not part of a combination")
	}
}

func TestCombination_SellableAndRange(t *testing.T) {
	p := comboProduct()
	sell := p.SellableCombinations()
	if len(sell) != 2 || sell[0].ID != "a" || sell[1].ID != "c" {
		t.Fatalf("only valid On rows are sellable, got %+v", sell)
	}
	lo, hi, ok := p.VariationPriceRange()
	if !ok || lo != 2500 || hi != 2999 {
		t.Fatalf("range over sellable rows: want 2500–2999, got %d–%d ok=%v", lo, hi, ok)
	}
	if !p.HasCombinations() {
		t.Fatal("HasCombinations")
	}
	if lo, hi := p.PriceRange(); lo != 2500 || hi != 2999 {
		t.Fatalf("PriceRange ignores base price for combination products, got %d–%d", lo, hi)
	}
}

func TestCombination_FindAndLabel(t *testing.T) {
	p := comboProduct()
	row := p.FindCombination([]string{"ve", "s10"})
	if row == nil || row.ID != "c" {
		t.Fatalf("find ignores order, got %+v", row)
	}
	if p.FindCombination([]string{"s10", "rv"}) != nil {
		t.Fatal("a choice with no row must not match")
	}
	if got := p.CombinationLabel(*row); got != "10 Inch | Vanilla Eggless" {
		t.Fatalf("label in group order, got %q", got)
	}
	if got := p.VariationOptionIDs([]string{"c", "s8", "ve"}); len(got) != 2 {
		t.Fatalf("add-on ids are filtered out, got %v", got)
	}
}
