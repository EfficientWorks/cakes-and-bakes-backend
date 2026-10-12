package domain

import (
	"context"
	"strings"
	"time"

	"bakehouse/pkg/apperr"
	"bakehouse/services/catalog/catalogapi"
)

// GetProductForStorefront returns full product detail plus its resolved
// availability and a small set of similar products from the same category.
func (s *Service) GetProductForStorefront(ctx context.Context, slug, branchID string, date time.Time) (*ProductWithAvailability, []catalogapi.Product, error) {
	p, err := s.store.GetProductBySlug(ctx, slug)
	if err != nil {
		p, err = s.store.GetProductByID(ctx, slug)
		if err != nil {
			return nil, nil, notFoundOrInternal(err, "Product not found.")
		}
	}

	similar, err := s.store.ListProducts(ctx, p.CategoryID, "", false)
	if err != nil {
		return nil, nil, apperr.Internal(err)
	}
	var similarOut []catalogapi.Product
	for _, sp := range similar {
		if sp.ID != p.ID {
			similarOut = append(similarOut, sp)
		}
		if len(similarOut) == 4 {
			break
		}
	}

	// Without a branch the product page still renders in full — only the
	// availability badge and any branch price override are withheld, because
	// neither can be computed without knowing which kitchen is making it.
	avail := unresolvedAvailability()
	if branchID != "" {
		branch, err := s.branches.GetBranch(ctx, branchID)
		if err != nil {
			return nil, nil, apperr.NotFound("Branch not found.")
		}

		categoryRule, err := s.store.GetBranchCategoryRule(ctx, branchID, p.CategoryID)
		if err != nil {
			return nil, nil, apperr.Internal(err)
		}

		if categoryRule != nil && !categoryRule.BranchEnabled {
			reason := catalogapi.ReasonCategoryNotMapped
			if categoryRule.Reason != nil && *categoryRule.Reason != "" {
				reason = *categoryRule.Reason
			}
			avail = catalogapi.Availability{Available: false, Reason: reason}
			return &ProductWithAvailability{Product: *p, Availability: avail}, similarOut, nil
		}

		productRule, err := s.store.GetBranchProductRule(ctx, branchID, p.ID)
		if err != nil {
			return nil, nil, apperr.Internal(err)
		}

		if productRule != nil && !productRule.BranchEnabled {
			reason := catalogapi.ReasonProductNotMapped
			if productRule.Reason != nil && *productRule.Reason != "" {
				reason = *productRule.Reason
			}
			avail = catalogapi.Availability{Available: false, Reason: reason}
			p.BasePriceCents = effectiveBranchBasePrice(p.BasePriceCents, productRule.PriceOverrideCents)
			return &ProductWithAvailability{Product: *p, Availability: avail}, similarOut, nil
		}

		if date.IsZero() {
			date = time.Now()
		}
		localDate := toLocalDate(date, branch.Timezone)
		rule := catalogapi.BranchProductRule{
			BranchID:      branchID,
			ProductID:     p.ID,
			BranchEnabled: false,
			AvailableDays: catalogapi.DefaultAvailableDays,
		}
		if productRule != nil {
			rule = *productRule
		}
		avail = resolveAvailability(*p, rule, localDate)

		// Apply the branch price override to the base price, but treat zero as
		// "not set" so the product's own base remains the effective price.
		if rule.BranchEnabled {
			p.BasePriceCents = effectiveBranchBasePrice(p.BasePriceCents, rule.PriceOverrideCents)
		}

		// Apply branch modifier rules price overrides
		var optionIDs []string
		for _, g := range p.ModifierGroups {
			for _, opt := range g.Options {
				optionIDs = append(optionIDs, opt.ID)
			}
		}
		if len(optionIDs) > 0 {
			modRules, err := s.store.GetBranchModifierRulesByOptionIDs(ctx, branchID, optionIDs)
			if err == nil && len(modRules) > 0 {
				for gi := range p.ModifierGroups {
					for oi := range p.ModifierGroups[gi].Options {
						optID := p.ModifierGroups[gi].Options[oi].ID
						if mr, ok := modRules[optID]; ok && mr.PriceOverrideCents != nil && *mr.PriceOverrideCents != 0 {
							p.ModifierGroups[gi].Options[oi].PriceDeltaCents = *mr.PriceOverrideCents
						}
					}
				}
			}
		}
	}

	return &ProductWithAvailability{Product: *p, Availability: avail}, similarOut, nil
}

type ProductWithAvailability struct {
	catalogapi.Product
	Availability catalogapi.Availability
}

// resolveAvailability implements the 4-layer cascade (BRD Section 8.1,
// CAT-04): the most specific layer that expresses an opinion wins.
//
//	Layer 4 — date-specific override (highest priority)
//	Layer 3 — day-of-week schedule
//	Layer 2 — branch carries this product at all
//	Layer 1 — master catalogue (product.is_active), the fallback
//
// Seasonal windows sit between layers 2 and 1 per the BRD's own worked
// example, and there are two of them: the branch's own season
// (branch_product_rules) and the product's, which applies everywhere it is
// sold. The branch's is the more specific, so it is asked first.
func resolveAvailability(p catalogapi.Product, rule catalogapi.BranchProductRule, date time.Time) catalogapi.Availability {
	dateStr := date.Format("2006-01-02")

	// Layer 4 — date-specific override (highest priority among scheduling layers).
	for _, o := range rule.DateOverrides {
		if o.Date == dateStr {
			return catalogapi.Availability{Available: o.Available, Reason: o.Reason}
		}
	}

	// Layer 3 — day-of-week schedule.
	dayName := strings.ToLower(date.Weekday().String())
	if len(rule.AvailableDays) > 0 && !containsDay(rule.AvailableDays, dayName) {
		return catalogapi.Availability{Available: false, Reason: "Not available on this day"}
	}

	// Layer 2 — branch carries this product at all.
	if !rule.BranchEnabled {
		reason := catalogapi.ReasonProductNotMapped
		if rule.Reason != nil && *rule.Reason != "" {
			reason = *rule.Reason
		}
		return catalogapi.Availability{Available: false, Reason: reason}
	}

	if rule.AvailableFrom != nil && date.Before(*rule.AvailableFrom) {
		next := rule.AvailableFrom.Format("2006-01-02")
		return catalogapi.Availability{Available: false, Reason: "Not yet in season", NextAvailableDate: &next}
	}
	if rule.AvailableUntil != nil && date.After(*rule.AvailableUntil) {
		return catalogapi.Availability{Available: false, Reason: "Season has ended"}
	}

	// The master-catalogue season — a product sold only for Mother's Day, or
	// only through December. Outside it the product is simply not for sale,
	// anywhere, however willing the branch is.
	if p.AvailableFrom != nil && dateStr < p.AvailableFrom.Format("2006-01-02") {
		next := p.AvailableFrom.Format("2006-01-02")
		return catalogapi.Availability{Available: false, Reason: "Not yet on sale", NextAvailableDate: &next}
	}
	if p.AvailableUntil != nil && dateStr > p.AvailableUntil.Format("2006-01-02") {
		return catalogapi.Availability{Available: false, Reason: "No longer on sale"}
	}

	if !p.IsActive {
		return catalogapi.Availability{Available: false, Reason: "Product discontinued"}
	}

	return catalogapi.Availability{Available: true}
}

func containsDay(days []string, day string) bool {
	for _, d := range days {
		if d == day {
			return true
		}
	}
	return false
}

func toLocalDate(t time.Time, tz string) time.Time {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	lt := t.In(loc)
	return time.Date(lt.Year(), lt.Month(), lt.Day(), 0, 0, 0, 0, time.UTC)
}
