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
	if !p.CombinationValid(sizeOnly) {
		t.Fatal("a row may use only some option sets")
	}
	if p.CombinationValid(ProductCombination{}) {
		t.Fatal("an empty row must be invalid")
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

func TestCombination_Problem(t *testing.T) {
	p := comboProduct()
	p.ModifierGroups[0].Name = "Size"
	p.ModifierGroups[1].DisplayName = "Sponge"
	if got := p.CombinationProblem(ProductCombination{OptionIDs: []string{"s8"}}); got != "" {
		t.Fatalf("a row using only some sets is fine, got %q", got)
	}
	if got := p.CombinationProblem(ProductCombination{OptionIDs: []string{"s8", "s10", "ve"}}); got != "more than one option from Size" {
		t.Fatalf("doubled set named, got %q", got)
	}
	if got := p.CombinationProblem(p.Combinations[0]); got != "" {
		t.Fatalf("valid row has no problem, got %q", got)
	}
}

func TestCombination_PartialRows(t *testing.T) {
	p := comboProduct()
	// Only 8 Inch offers a sponge choice; 10 Inch is sold on its own.
	p.Combinations = []ProductCombination{
		{ID: "x", OptionIDs: NormaliseOptionIDs([]string{"s8", "rv"}), IsActive: true},
		{ID: "y", OptionIDs: []string{"s10"}, IsActive: true},
	}
	if row := p.FindCombination([]string{"s10"}); row == nil || row.ID != "y" {
		t.Fatalf("size-only choice matches the size-only row, got %+v", row)
	}
	if p.FindCombination([]string{"s8"}) != nil {
		t.Fatal("8 Inch alone is not a row: the sponge must be chosen")
	}
	if p.FindCombination([]string{"s10", "ve"}) != nil {
		t.Fatal("a choice with an extra option must not match a shorter row")
	}
	if got := p.CombinationAutoPrice(p.Combinations[1]); got != 3999 {
		t.Fatalf("auto price of a partial row sums its own options, got %d", got)
	}
	if got := p.CombinationLabel(p.Combinations[0]); got != "8 Inch | Red Velvet with Egg" {
		t.Fatalf("label, got %q", got)
	}
}

func TestCombination_FromPriceSkipsZero(t *testing.T) {
	p := comboProduct()
	zero := int64(0)
	p.Combinations = []ProductCombination{
		{ID: "z", OptionIDs: NormaliseOptionIDs([]string{"s8", "ve"}), IsActive: true, PriceOverrideCents: &zero},
		{ID: "a", OptionIDs: NormaliseOptionIDs([]string{"s10", "ve"}), IsActive: true},
	}
	if lo, hi, _ := p.CombinationPriceRange(); lo != 3999 || hi != 3999 {
		t.Fatalf("a £0 row is not the From price: want 3999–3999, got %d–%d", lo, hi)
	}
	p.Combinations = p.Combinations[:1]
	if lo, _, ok := p.CombinationPriceRange(); lo != 0 || !ok {
		t.Fatalf("every row £0 means From £0, got %d ok=%v", lo, ok)
	}
}
