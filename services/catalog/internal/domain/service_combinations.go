package domain

import (
	"context"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/tenant"
	"bakehouse/services/catalog/catalogapi"
)

// maxCombinations caps one product's rows: 4 sizes × 5 sponges × 5 creams is
// 100, so 500 leaves room without letting a typo build a million rows.
const maxCombinations = 500

// CombinationInput is one row as the console sends it.
type CombinationInput struct {
	OptionIDs  []string
	PriceCents *int64 // nil = automatic (sum of the options' prices)
	IsActive   bool
}

// ProductCombinations returns a product with its groups and rows loaded, for
// the console's Variations Set screen.
func (s *Service) ProductCombinations(ctx context.Context, claims *auth.StaffClaims, productID string) (*catalogapi.Product, error) {
	if !tenant.HasPermission(claims, tenant.PermCatalogueEditMaster) {
		return nil, apperr.PermissionDenied("Only super admin can view a product's combinations.")
	}
	p, err := s.store.GetProductByID(ctx, productID)
	if err != nil {
		return nil, notFoundOrInternal(err, "Product not found.")
	}
	return p, nil
}

// SetProductCombinations replaces a product's option-set rows.
//
// Each row holds at most one option from each variation group the product
// carries right now (a row may use only some of them) — so the console attaches the option sets first
// (PUT /admin/products/{id}/modifiers) and saves the rows second. An empty
// list removes every row and the product goes back to being priced the old
// way.
func (s *Service) SetProductCombinations(ctx context.Context, claims *auth.StaffClaims, productID string, rows []CombinationInput) (*catalogapi.Product, error) {
	if !tenant.HasPermission(claims, tenant.PermCatalogueEditMaster) {
		return nil, apperr.PermissionDenied("Only super admin can change a product's combinations.")
	}
	if len(rows) > maxCombinations {
		return nil, apperr.New("TOO_MANY_COMBINATIONS", apperr.CategoryValidation,
			"A product can have at most "+itoa(maxCombinations)+" combinations.")
	}
	p, err := s.store.GetProductByID(ctx, productID)
	if err != nil {
		return nil, notFoundOrInternal(err, "Product not found.")
	}

	combos := make([]catalogapi.ProductCombination, 0, len(rows))
	seen := make(map[string]int, len(rows))
	for i, r := range rows {
		c := catalogapi.ProductCombination{
			OptionIDs:          catalogapi.NormaliseOptionIDs(r.OptionIDs),
			PriceOverrideCents: r.PriceCents,
			IsActive:           r.IsActive,
		}
		field := "combinations[" + itoa(i) + "]"
		if r.PriceCents != nil && *r.PriceCents < 0 {
			return nil, apperr.New("INVALID_PRICE", apperr.CategoryValidation, "A combination price cannot be negative.").
				WithFields(apperr.FieldError{Field: field + ".priceCents", Code: "MIN", Message: "Price cannot be negative."})
		}
		if !p.CombinationValid(c) {
			problem := p.CombinationProblem(c)
			return nil, apperr.New("INVALID_COMBINATION", apperr.CategoryValidation,
				"Combination "+itoa(i+1)+" is not valid: "+problem+". A combination takes at most one option from each option set on this product.").
				WithFields(apperr.FieldError{Field: field + ".optionIds", Code: "INVALID", Message: "Use at most one option from each option set (" + problem + ")."})
		}
		key := ""
		for _, id := range c.OptionIDs {
			key += id + ","
		}
		if prev, dup := seen[key]; dup {
			return nil, apperr.New("DUPLICATE_COMBINATION", apperr.CategoryValidation,
				"Combinations "+itoa(prev+1)+" and "+itoa(i+1)+" are the same.").
				WithFields(apperr.FieldError{Field: field + ".optionIds", Code: "DUPLICATE", Message: "This combination is already in the list."})
		}
		seen[key] = i
		combos = append(combos, c)
	}

	if err := s.store.ReplaceProductCombinations(ctx, productID, combos); err != nil {
		return nil, apperr.Internal(err)
	}
	s.writeAudit(ctx, claims, "product.combinations", "product", &productID, nil, nil,
		map[string]any{"count": len(combos)})

	updated, err := s.store.GetProductByID(ctx, productID)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return updated, nil
}
