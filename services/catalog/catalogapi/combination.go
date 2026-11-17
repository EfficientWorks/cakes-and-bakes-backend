package catalogapi

import (
	"sort"
	"strings"
)

// ProductCombination is one way a product can be bought when it is sold by
// option sets: one option from each of its variation groups, e.g.
// "8 Inch | Vanilla Eggless | Vanilla Fresh Cream".
//
// The price is automatic — the sum of the chosen options' prices, after any
// store price — until staff type one; PriceOverrideCents then wins. IsActive
// is the console's On/Off: an Off row is never offered or sold. There is no
// stock: a row is either on sale or not.
type ProductCombination struct {
	ID                 string
	OptionIDs          []string // sorted; see NormaliseOptionIDs
	PriceOverrideCents *int64
	IsActive           bool
	SortOrder          int
}

// NormaliseOptionIDs trims, de-duplicates and sorts option ids, so the same
// choice always produces the same stored key whatever order it arrived in.
func NormaliseOptionIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.ToLower(strings.TrimSpace(id))
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func combinationKey(ids []string) string {
	return strings.Join(NormaliseOptionIDs(ids), ",")
}

// HasCombinations reports whether the product is sold by option-set rows.
// Such a product is priced by its rows, whatever its pricing mode says.
func (p Product) HasCombinations() bool { return len(p.Combinations) > 0 }

type variationRef struct {
	group  int // index into the variation groups, in display order
	option *ModifierOption
}

// variationIndex maps every active option of a variation group to its group,
// and counts the variation groups that have something to choose.
func (p Product) variationIndex() (map[string]variationRef, int) {
	idx := make(map[string]variationRef)
	groups := 0
	for gi := range p.ModifierGroups {
		g := &p.ModifierGroups[gi]
		if g.Kind != ModifierKindVariation || len(g.Options) == 0 {
			continue
		}
		for oi := range g.Options {
			idx[strings.ToLower(g.Options[oi].ID)] = variationRef{group: groups, option: &g.Options[oi]}
		}
		groups++
	}
	return idx, groups
}

// combinationRefs resolves a row's options in group order, or returns false
// when the row no longer describes a valid choice: it is empty, an option was
// removed, or two options come from the same group.
//
// A row need not use every option set: "8 Inch | Vanilla | Butter Cream" can
// sit beside "10 Inch | Vanilla" when only the first offers a filling. The
// customer's choice has to match a row exactly, which is what keeps that
// unambiguous.
func (p Product) combinationRefs(c ProductCombination) ([]variationRef, bool) {
	idx, groups := p.variationIndex()
	if groups == 0 || len(c.OptionIDs) == 0 || len(c.OptionIDs) > groups {
		return nil, false
	}
	byGroup := make([]*variationRef, groups)
	for _, id := range c.OptionIDs {
		ref, ok := idx[strings.ToLower(id)]
		if !ok || byGroup[ref.group] != nil {
			return nil, false
		}
		r := ref
		byGroup[ref.group] = &r
	}
	refs := make([]variationRef, 0, len(c.OptionIDs))
	for _, r := range byGroup {
		if r != nil {
			refs = append(refs, *r)
		}
	}
	return refs, true
}

// CombinationProblem explains why a row is not valid, naming the option sets
// involved ("more than one option from Size"), or "" when it is valid. It is what
// the console shows, so a refused save says which set is missing.
func (p Product) CombinationProblem(c ProductCombination) string {
	idx, _ := p.variationIndex()
	var groups []*ModifierGroup
	for gi := range p.ModifierGroups {
		g := &p.ModifierGroups[gi]
		if g.Kind == ModifierKindVariation && len(g.Options) > 0 {
			groups = append(groups, g)
		}
	}
	if len(groups) == 0 {
		return "this product has no option sets attached"
	}
	name := func(g *ModifierGroup) string {
		if strings.TrimSpace(g.DisplayName) != "" {
			return g.DisplayName
		}
		return g.Name
	}
	count := make([]int, len(groups))
	for _, id := range c.OptionIDs {
		ref, ok := idx[strings.ToLower(id)]
		if !ok {
			return "an option in it is not in any option set on this product (removed or inactive)"
		}
		count[ref.group]++
	}
	if len(c.OptionIDs) == 0 {
		return "it has no options"
	}
	var doubled []string
	for i, g := range groups {
		if count[i] > 1 {
			doubled = append(doubled, name(g))
		}
	}
	if len(doubled) > 0 {
		return "more than one option from " + strings.Join(doubled, ", ")
	}
	return ""
}

// CombinationValid reports whether every option in the row still exists and
// the row has exactly one option from each variation group.
func (p Product) CombinationValid(c ProductCombination) bool {
	_, ok := p.combinationRefs(c)
	return ok
}

// CombinationAutoPrice is the row's automatic price: the sum of its options'
// prices as loaded (so a branch's store prices apply when the caller has
// applied them). Zero when no option carries a price.
func (p Product) CombinationAutoPrice(c ProductCombination) int64 {
	refs, ok := p.combinationRefs(c)
	if !ok {
		return 0
	}
	var sum int64
	for _, r := range refs {
		sum += r.option.PriceDeltaCents
	}
	return sum
}

// CombinationPrice is what the customer pays for the row before add-ons: the
// typed price when there is one, the automatic price otherwise.
func (p Product) CombinationPrice(c ProductCombination) int64 {
	if c.PriceOverrideCents != nil {
		return *c.PriceOverrideCents
	}
	return p.CombinationAutoPrice(c)
}

// CombinationLabel names the row in group order: "8 Inch | Vanilla Eggless |
// Vanilla Fresh Cream". Empty for a row that is no longer valid.
func (p Product) CombinationLabel(c ProductCombination) string {
	refs, ok := p.combinationRefs(c)
	if !ok {
		return ""
	}
	names := make([]string, len(refs))
	for i, r := range refs {
		names[i] = r.option.Name
	}
	return strings.Join(names, " | ")
}

// SellableCombinations are the rows a customer may choose: valid and On.
func (p Product) SellableCombinations() []ProductCombination {
	out := make([]ProductCombination, 0, len(p.Combinations))
	for _, c := range p.Combinations {
		if c.IsActive && p.CombinationValid(c) {
			out = append(out, c)
		}
	}
	return out
}

// CombinationPriceRange is the cheapest and dearest sellable row. ok is false
// when no row can be sold.
//
// The cheapest is the lowest price above zero: a row nobody has priced yet
// (£0.00) must not make the shop say "From £0.00". Only when every row is
// £0.00 is the minimum zero.
func (p Product) CombinationPriceRange() (minCents, maxCents int64, ok bool) {
	var minPositive int64
	for i, c := range p.SellableCombinations() {
		price := p.CombinationPrice(c)
		if price > 0 && (minPositive == 0 || price < minPositive) {
			minPositive = price
		}
		if i == 0 || price > maxCents {
			maxCents = price
		}
		ok = true
	}
	return minPositive, maxCents, ok
}

// FindCombination returns the valid row made of exactly these variation
// options (in any order), On or Off, or nil when there is none.
func (p Product) FindCombination(optionIDs []string) *ProductCombination {
	key := combinationKey(optionIDs)
	for i := range p.Combinations {
		c := p.Combinations[i]
		if combinationKey(c.OptionIDs) == key && p.CombinationValid(c) {
			return &p.Combinations[i]
		}
	}
	return nil
}

// VariationOptionIDs keeps only the ids that belong to the product's
// variation groups — the part of a selection a combination is made of.
func (p Product) VariationOptionIDs(ids []string) []string {
	idx, _ := p.variationIndex()
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := idx[strings.ToLower(id)]; ok {
			out = append(out, id)
		}
	}
	return out
}
