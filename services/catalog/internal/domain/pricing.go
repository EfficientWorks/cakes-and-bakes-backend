package domain

import (
	"context"
	"time"

	"bakehouse/pkg/apperr"
	"bakehouse/services/catalog/catalogapi"
)

// GetPricedItem resolves and prices one line item from customer-selected
// ids, used both when adding to cart and again at checkout (re-validating
// price and availability have not changed since — SF-13, checkout/validate
// in 05-ORDERS.md). It never trusts a client-supplied price.
func effectiveBranchBasePrice(productBaseCents int64, override *int64) int64 {
	if override != nil && *override != 0 {
		return *override
	}
	return productBaseCents
}

func (s *Service) GetPricedItem(ctx context.Context, branchID, productID string, variantID *string, flavourIDs, addonIDs []string, presentationID, wrapColourID *string, date time.Time) (*catalogapi.PricedItem, error) {
	product, err := s.store.GetProductByID(ctx, productID)
	if err != nil {
		return nil, notFoundOrInternal(err, "Product not found.")
	}

	branch, err := s.branches.GetBranch(ctx, branchID)
	if err != nil {
		return nil, apperr.NotFound("Branch not found.")
	}
	categoryRule, err := s.store.GetBranchCategoryRule(ctx, branchID, product.CategoryID)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if categoryRule != nil && !categoryRule.BranchEnabled {
		reason := catalogapi.ReasonCategoryNotMapped
		if categoryRule.Reason != nil && *categoryRule.Reason != "" {
			reason = *categoryRule.Reason
		}
		out := &catalogapi.PricedItem{
			ProductID: product.ID, ProductName: product.Name, UnitPriceCents: product.BasePriceCents,
			Available: false, AvailableReason: reason,
		}
		if len(product.Images) > 0 {
			out.ProductImageURL = &product.Images[0].URL
		}
		return out, nil
	}

	rule, err := s.store.GetBranchProductRule(ctx, branchID, productID)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if rule != nil && !rule.BranchEnabled {
		reason := catalogapi.ReasonProductNotMapped
		if rule.Reason != nil && *rule.Reason != "" {
			reason = *rule.Reason
		}
		out := &catalogapi.PricedItem{
			ProductID: product.ID, ProductName: product.Name, UnitPriceCents: product.BasePriceCents,
			Available: false, AvailableReason: reason,
		}
		if len(product.Images) > 0 {
			out.ProductImageURL = &product.Images[0].URL
		}
		return out, nil
	}
	// No record → GetBranchProductRule returns BranchEnabled=false (unavailable).
	// BranchEnabled=false → returns Available=false (checked above).
	// BranchEnabled=true  → resolveAvailability checks scheduling layers normally.
	localDate := toLocalDate(date, branch.Timezone)
	avail := resolveAvailability(*product, *rule, localDate)

	// Apply branch price override only when the branch actively sells this
	// product. A zero override is treated as “unset”, so the product's own base
	// price remains in force.
	basePrice := product.BasePriceCents
	if rule.BranchEnabled {
		basePrice = effectiveBranchBasePrice(product.BasePriceCents, rule.PriceOverrideCents)
	}

	/*
	 * A variation-priced product is charged for what was picked, not for the
	 * product itself: the line starts at zero and the selected variation
	 * options (resolved below, exactly as before) add up to its price. The
	 * base price is only a fallback for a product flagged this way that has
	 * no variation group to price from — charging nothing would be worse than
	 * charging the catalogue price.
	 *
	 * The cart keeps the base and the modifier deltas apart and adds them back
	 * together with UnitTotalCents(), so a zero base with the option prices
	 * beside it reads back to the same number this returns.
	 */
	if _, _, hasVariations := product.VariationPriceRange(); product.IsVariationPriced() && hasVariations {
		basePrice = 0
	}

	out := &catalogapi.PricedItem{
		ProductID: product.ID, ProductName: product.Name, UnitPriceCents: basePrice,
		PricingMode: product.EffectivePricingMode(),
		Available:   avail.Available, AvailableReason: avail.Reason,
	}
	if len(product.Images) > 0 {
		out.ProductImageURL = &product.Images[0].URL
	}
	// if variantID != nil {
	// 	var found *catalogapi.ProductVariant
	// 	for i := range product.Variants {
	// 		if product.Variants[i].ID == *variantID {
	// 			found = &product.Variants[i]
	// 			break
	// 		}
	// 	}
	// 	if found == nil {
	// 		return nil, apperr.New("VARIANT_NOT_FOUND", apperr.CategoryValidation, "Selected size is not available for this product.")
	// 	}
	// 	out.VariantID = &found.ID
	// 	out.VariantName = &found.Name
	// 	out.UnitPriceCents += found.PriceDeltaCents
	// }

	/*
	 * Resolves every selection of one type.
	 *
	 * A product can carry more than one group of the same type — two flavour
	 * groups, say, where the console attached "Flavour Assortment" alongside
	 * "Flavours". So the selections are first partitioned by the group that
	 * actually owns each option, and every group is then checked against its
	 * own minimum and maximum.
	 *
	 * This used to take the first group of the type and validate the whole
	 * selection against it, which made a second group unusable: its options
	 * were unknown to the first group, and the combined count blew a maximum
	 * that only applied to one of them.
	 */
	seenIDs := make(map[string]struct{}, len(flavourIDs)+len(addonIDs)+2)
	allRequestedIDs := make([]string, 0, len(flavourIDs)+len(addonIDs)+2)
	for _, id := range flavourIDs {
		if _, ok := seenIDs[id]; !ok {
			seenIDs[id] = struct{}{}
			allRequestedIDs = append(allRequestedIDs, id)
		}
	}
	for _, id := range addonIDs {
		if _, ok := seenIDs[id]; !ok {
			seenIDs[id] = struct{}{}
			allRequestedIDs = append(allRequestedIDs, id)
		}
	}
	if presentationID != nil && *presentationID != "" {
		if _, ok := seenIDs[*presentationID]; !ok {
			seenIDs[*presentationID] = struct{}{}
			allRequestedIDs = append(allRequestedIDs, *presentationID)
		}
	}
	if wrapColourID != nil && *wrapColourID != "" {
		if _, ok := seenIDs[*wrapColourID]; !ok {
			seenIDs[*wrapColourID] = struct{}{}
			allRequestedIDs = append(allRequestedIDs, *wrapColourID)
		}
	}

	var branchModifierRules map[string]catalogapi.BranchModifierRule
	if branchID != "" && len(allRequestedIDs) > 0 {
		var err error
		branchModifierRules, err = s.store.GetBranchModifierRulesByOptionIDs(ctx, branchID, allRequestedIDs)
		if err != nil {
			return nil, apperr.Internal(err)
		}
	}

	/*
	 * resolveGroup finds all modifier groups matched by the predicate, then
	 * validates and collects the chosen options from wantIDs.
	 *
	 * required is now driven by each group's own IsRequired flag rather than
	 * a blanket parameter: a caller that does not know which groups are
	 * mandatory does not have to guess.
	 *
	 * A selection the product no longer offers is dropped, not refused.
	 * Cart lines carry a snapshot of what was chosen. When an admin edits
	 * the product afterwards the line still named an option the product could
	 * no longer explain, and checkout answered MODIFIER_GROUP_NOT_FOUND with
	 * nothing the customer could do about it. Dropping it charges the honest
	 * price for what the product actually offers today; groups that still
	 * exist are validated exactly as before, so a required choice is still
	 * required.
	 */
	resolveGroup := func(matches func(*catalogapi.ModifierGroup) bool, wantIDs []string) ([]catalogapi.SelectedModifier, error) {
		groups := make([]*catalogapi.ModifierGroup, 0, 2)
		for i := range product.ModifierGroups {
			if matches(&product.ModifierGroups[i]) {
				groups = append(groups, &product.ModifierGroups[i])
			}
		}
		if len(groups) == 0 {
			return nil, nil
		}

		// Which group owns each selected option. An id no group recognises is
		// silently dropped — withdrawn since it was chosen.
		owner := make(map[string]*catalogapi.ModifierGroup, len(wantIDs))
		option := make(map[string]*catalogapi.ModifierOption, len(wantIDs))
		perGroup := make(map[string]int, len(groups))
		for _, id := range wantIDs {
			for _, g := range groups {
				for i := range g.Options {
					if g.Options[i].ID == id {
						owner[id] = g
						option[id] = &g.Options[i]
						break
					}
				}
				if owner[id] != nil {
					break
				}
			}
			if owner[id] == nil {
				continue
			}
			perGroup[owner[id].ID]++
		}

		for _, g := range groups {
			n := perGroup[g.ID]
			// Use the group's own IsRequired rather than a caller-supplied flag:
			// the group itself knows whether a selection is mandatory.
			if g.IsRequired && g.MinSelect > 0 && n < g.MinSelect {
				return nil, apperr.New("MODIFIER_REQUIRED", apperr.CategoryValidation, g.Name+" requires at least "+itoa(g.MinSelect)+" selection(s).").
					WithFields(apperr.FieldError{Field: g.Type, Code: "MIN_SELECT", Message: g.Name + " is required."})
			}
			if g.MaxSelect > 0 && n > g.MaxSelect {
				return nil, apperr.New("MODIFIER_TOO_MANY", apperr.CategoryValidation, "Too many "+g.Name+" selected (max "+itoa(g.MaxSelect)+").")
			}
		}

		out := make([]catalogapi.SelectedModifier, 0, len(wantIDs))
		for _, id := range wantIDs {
			opt := option[id]
			if opt == nil {
				continue
			}
			priceDelta := opt.PriceDeltaCents
			if branchModifierRules != nil {
				if rule, ok := branchModifierRules[opt.ID]; ok && rule.PriceOverrideCents != nil {
					priceDelta = *rule.PriceOverrideCents
				}
			}
			out = append(out, catalogapi.SelectedModifier{ID: opt.ID, Name: opt.Name, PriceDelta: priceDelta})
		}
		return out, nil
	}

	/*
	 * Modifier resolution by kind.
	 *
	 * kind="variation" → selectedFlavours on the line.
	 *   These are "pick one from a set" groups: Presentation, Wrap Colour, or
	 *   any group the admin marks as a variation. They affect the product's
	 *   form, not just its contents.
	 *
	 * kind="addon" → selectedAddons on the line.
	 *   These are "add any number of extras" groups: Flavours, toppers, or
	 *   any optional / required add-on regardless of its storefront type label.
	 *
	 * The caller (POS or storefront) may bucket the chosen option IDs by kind
	 * or by storefront type — both conventions are handled by merging all
	 * supplied IDs into one deduplicated pool and letting each option's owning
	 * group determine which resolver validates it. This means the caller does
	 * not have to know the internal type taxonomy; supplying the correct option
	 * IDs is enough.
	 */
	seen := make(map[string]struct{}, len(flavourIDs)+len(addonIDs))
	allModifierIDs := make([]string, 0, len(flavourIDs)+len(addonIDs))
	for _, id := range flavourIDs {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			allModifierIDs = append(allModifierIDs, id)
		}
	}
	for _, id := range addonIDs {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			allModifierIDs = append(allModifierIDs, id)
		}
	}

	var presentationIDs, wrapColourIDs []string
	if presentationID != nil && *presentationID != "" {
		presentationIDs = []string{*presentationID}
	}
	if wrapColourID != nil && *wrapColourID != "" {
		wrapColourIDs = []string{*wrapColourID}
	}

	// Presentation and wrap-colour can be passed as discrete single-ID params on
	// the storefront path; on the POS path both arrive as nil and choices travel
	// in selectedFlavours/selectedAddons. When discrete IDs are passed, resolve
	// them into SelectedPresentation/SelectedWrapColour and exclude those groups
	// from the general variation/addon resolvers so they are not validated twice
	// or double-counted.
	hasDedicatedPresentation := len(presentationIDs) > 0
	hasDedicatedWrapColour := len(wrapColourIDs) > 0

	var presentation, wrapColour []catalogapi.SelectedModifier
	if hasDedicatedPresentation {
		var err error
		presentation, err = resolveGroup(func(g *catalogapi.ModifierGroup) bool {
			return g.Type == catalogapi.ModifierPresentation
		}, presentationIDs)
		if err != nil {
			return nil, err
		}
		if len(presentation) == 1 {
			out.UnitPriceCents += presentation[0].PriceDelta
			out.SelectedPresentation = &presentation[0]
		}
	}

	if hasDedicatedWrapColour {
		var err error
		wrapColour, err = resolveGroup(func(g *catalogapi.ModifierGroup) bool {
			return g.Type == catalogapi.ModifierWrapColour
		}, wrapColourIDs)
		if err != nil {
			return nil, err
		}
		if len(wrapColour) == 1 {
			out.UnitPriceCents += wrapColour[0].PriceDelta
			out.SelectedWrapColour = &wrapColour[0]
		}
	}

	flavours, err := resolveGroup(func(g *catalogapi.ModifierGroup) bool {
		if hasDedicatedPresentation && g.Type == catalogapi.ModifierPresentation {
			return false
		}
		if hasDedicatedWrapColour && g.Type == catalogapi.ModifierWrapColour {
			return false
		}
		return g.Kind == catalogapi.ModifierKindVariation
	}, allModifierIDs)
	if err != nil {
		return nil, err
	}
	for _, f := range flavours {
		out.UnitPriceCents += f.PriceDelta
	}

	addons, err := resolveGroup(func(g *catalogapi.ModifierGroup) bool {
		if hasDedicatedPresentation && g.Type == catalogapi.ModifierPresentation {
			return false
		}
		if hasDedicatedWrapColour && g.Type == catalogapi.ModifierWrapColour {
			return false
		}
		return g.Kind == catalogapi.ModifierKindAddon
	}, allModifierIDs)
	if err != nil {
		return nil, err
	}
	for _, a := range addons {
		out.UnitPriceCents += a.PriceDelta
	}

	out.SelectedFlavours = flavours
	out.SelectedAddons = addons

	return out, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
