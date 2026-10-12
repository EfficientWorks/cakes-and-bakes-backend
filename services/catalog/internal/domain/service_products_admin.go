package domain

import (
	"context"
	"errors"
	"strings"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/tenant"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/catalog/internal/store"
)

// --- Master catalogue (super admin only — governance: product.core_fields) ----

func (s *Service) CreateProduct(ctx context.Context, claims *auth.StaffClaims, p catalogapi.Product) (*catalogapi.Product, error) {
	if !tenant.HasPermission(claims, tenant.PermCatalogueEditMaster) {
		return nil, apperr.PermissionDenied("Only super admin can edit the master catalogue.")
	}
	if err := validateProduct(p); err != nil {
		return nil, err
	}
	out, err := s.store.CreateProduct(ctx, p)
	if err != nil {
		return nil, productWriteError(err, p.Slug)
	}
	if p.SeasonalTagIDs != nil || len(p.OccasionTags) > 0 {
		if err := s.applyProductTags(ctx, out.ID, p.SeasonalTagIDs, p.OccasionTags, true); err != nil {
			return nil, err
		}
		if out, err = s.store.GetProductByID(ctx, out.ID); err != nil {
			return nil, apperr.Internal(err)
		}
	}
	s.writeAudit(ctx, claims, "product.create", "product", &out.ID, nil, nil, out)
	return out, nil
}

// productWriteError turns a constraint failure into a sentence the person
// filling the form can act on.
//
// All three of these used to be "an unexpected error occurred", which is the
// least useful thing to say about a problem the caller can fix themselves —
// a name already used, a category that no longer exists, an id that is not an
// id at all.
func productWriteError(err error, slug string) error {
	switch {
	case errors.Is(err, store.ErrDuplicate):
		return apperr.New("PRODUCT_SLUG_TAKEN", apperr.CategoryConflict,
			"A product already uses the web address \""+slug+"\". Change the name or the slug.").
			WithFields(apperr.FieldError{Field: "slug", Code: "TAKEN", Message: "Already in use."})
	case errors.Is(err, store.ErrRelatedMissing):
		return apperr.Validation("That category no longer exists. Choose another.").
			WithFields(apperr.FieldError{Field: "categoryId", Code: "NOT_FOUND", Message: "Choose a category."})
	case errors.Is(err, store.ErrMalformedID):
		return apperr.Validation("The category is not valid. Choose one from the list.").
			WithFields(apperr.FieldError{Field: "categoryId", Code: "INVALID", Message: "Choose a category."})
	default:
		return apperr.Internal(err)
	}
}

// UpdateProduct applies a partial change to a master-catalogue product.
//
// It is a genuine PATCH: the caller sends only what it is changing, and
// everything absent is carried over from the stored record. It used to demand
// a whole product, which made the console's one-tap availability toggle — the
// single most-used control in the catalogue (CAT-05, CAT-14) — fail with
// "name, slug and category are required" for sending exactly the one field it
// meant to change.
//
// Merging before validating is what makes that work: validation still insists
// on a name, a slug and a category, but checks them against the record as it
// will be, not against the fragment that arrived.
func (s *Service) UpdateProduct(ctx context.Context, claims *auth.StaffClaims, id string, patch catalogapi.ProductPatch) (*catalogapi.Product, error) {
	if !tenant.HasPermission(claims, tenant.PermCatalogueEditMaster) {
		return nil, apperr.PermissionDenied("Only super admin can edit the master catalogue.")
	}
	before, err := s.store.GetProductByID(ctx, id)
	if err != nil {
		return nil, notFoundOrInternal(err, "Product not found.")
	}
	p := patch.Apply(*before)
	if err := validateProduct(p); err != nil {
		return nil, err
	}
	out, err := s.store.UpdateProduct(ctx, id, p)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, apperr.NotFound("Product not found.")
		}
		return nil, productWriteError(err, p.Slug)
	}
	if patch.SeasonalTagIDs != nil || patch.OccasionTags != nil {
		if err := s.applyProductTags(ctx, id, p.SeasonalTagIDs, p.OccasionTags, patch.OccasionTags != nil); err != nil {
			return nil, err
		}
		if out, err = s.store.GetProductByID(ctx, id); err != nil {
			return nil, apperr.Internal(err)
		}
	}
	s.writeAudit(ctx, claims, "product.update", "product", &id, nil, before, out)
	return out, nil
}

func validateProduct(p catalogapi.Product) error {
	if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Slug) == "" || p.CategoryID == "" {
		return apperr.Validation("Product name, slug and category are required.")
	}
	if p.PricingMode != "" && !catalogapi.ValidPricingMode(p.PricingMode) {
		return apperr.Validation("pricingMode must be \"product\" or \"variation\".").
			WithFields(apperr.FieldError{Field: "pricingMode", Code: "INVALID", Message: "Use product or variation."})
	}
	if p.BasePriceCents < 0 || (!p.IsVariationPriced() && p.BasePriceCents == 0) {
		return apperr.Validation("Base price must be greater than zero.").
			WithFields(apperr.FieldError{Field: "basePriceCents", Code: "INVALID", Message: "Must be a positive integer."})
	}
	if p.AvailableFrom != nil && p.AvailableUntil != nil && p.AvailableUntil.Before(*p.AvailableFrom) {
		return apperr.Validation("The end date cannot fall before the start date.").
			WithFields(apperr.FieldError{Field: "availableUntil", Code: "INVALID", Message: "Must be on or after the start date."})
	}
	return validatePrepTime(p)
}

// validatePrepTime checks the KDS preparation window: known units, no
// negatives, and a minimum that does not exceed the maximum.
func validatePrepTime(p catalogapi.Product) error {
	if _, ok := catalogapi.NormalizePrepUnit(p.MinPrepTimeUnit); !ok {
		return apperr.Validation("minPrepTimeUnit must be minutes or hours.").
			WithFields(apperr.FieldError{Field: "minPrepTimeUnit", Code: "INVALID", Message: "minutes or hours."})
	}
	if _, ok := catalogapi.NormalizePrepUnit(p.MaxPrepTimeUnit); !ok {
		return apperr.Validation("maxPrepTimeUnit must be minutes or hours.").
			WithFields(apperr.FieldError{Field: "maxPrepTimeUnit", Code: "INVALID", Message: "minutes or hours."})
	}
	if p.MinPrepTime < 0 || p.MaxPrepTime < 0 {
		return apperr.Validation("Preparation time cannot be negative.").
			WithFields(apperr.FieldError{Field: "minPrepTime", Code: "INVALID", Message: "Must be zero or more."})
	}
	if catalogapi.PrepMinutes(p.MinPrepTime, p.MinPrepTimeUnit) > catalogapi.PrepMinutes(p.MaxPrepTime, p.MaxPrepTimeUnit) {
		return apperr.Validation("Minimum preparation time cannot be longer than the maximum.").
			WithFields(apperr.FieldError{Field: "maxPrepTime", Code: "INVALID", Message: "Must be at least the minimum."})
	}
	return nil
}

func (s *Service) DeleteProduct(ctx context.Context, claims *auth.StaffClaims, id string) error {
	if !tenant.HasPermission(claims, tenant.PermCatalogueEditMaster) {
		return apperr.PermissionDenied("Only super admin can edit the master catalogue.")
	}
	if err := s.store.SoftDeleteProduct(ctx, id); err != nil {
		return notFoundOrInternal(err, "Product not found.")
	}
	s.writeAudit(ctx, claims, "product.delete", "product", &id, nil, nil, nil)
	return nil
}

func (s *Service) GetProductAdmin(ctx context.Context, claims *auth.StaffClaims, id string, targetBranchID ...string) (*catalogapi.Product, error) {
	if !tenant.HasPermission(claims, tenant.PermCatalogueView) {
		return nil, apperr.PermissionDenied("You do not have permission to view the catalogue.")
	}
	p, err := s.store.GetProductByID(ctx, id)
	if err != nil {
		return nil, notFoundOrInternal(err, "Product not found.")
	}

	// 1. Super-admin and finance see master catalogue prices and all storeprices across the network.
	if tenant.IsSuperAdmin(claims) || tenant.IsFinance(claims) {
		return p, nil
	}

	// 2. Franchise owner / franchise viewer (now always branch-scoped):
	// Product price like admin, with storeprices filtered to only own branches.
	if tenant.HasRole(claims, tenant.RoleFranchiseOwner, tenant.RoleFranchiseViewer) {
		ownBranchMap := make(map[string]bool)
		for _, ra := range claims.RoleAssignments {
			if ra.BranchID != nil && *ra.BranchID != "" {
				ownBranchMap[*ra.BranchID] = true
			}
		}

		// Filter product storePrices to only own branches
		filteredProductPrices := make([]catalogapi.BranchProductRule, 0)
		for _, sp := range p.StorePrices {
			if ownBranchMap[sp.BranchID] {
				filteredProductPrices = append(filteredProductPrices, sp)
			}
		}
		p.StorePrices = filteredProductPrices

		// Filter modifier options' storePrices to only own branches
		for gi := range p.ModifierGroups {
			for oi := range p.ModifierGroups[gi].Options {
				filteredOptPrices := make([]catalogapi.BranchModifierRule, 0)
				for _, sp := range p.ModifierGroups[gi].Options[oi].StorePrices {
					if ownBranchMap[sp.BranchID] {
						filteredOptPrices = append(filteredOptPrices, sp)
					}
				}
				p.ModifierGroups[gi].Options[oi].StorePrices = filteredOptPrices
			}
		}

		p.BranchIds = s.visibleBranchIDsForClaims(ctx, claims, p.BranchIds)

		return p, nil
	}

	// 3. Branch-scoped roles (branch_manager / branchowner, etc.):
	// "branchowner base price come from branchproductrule"
	resolvedBranchID := s.ResolveBranchIDForStaff(ctx, claims, targetBranchID...)

	if resolvedBranchID != "" {
		// 1. Branch product rule price override
		productRule, err := s.store.GetBranchProductRule(ctx, resolvedBranchID, p.ID)
		if err == nil && productRule != nil {
			p.BasePriceCents = effectiveBranchBasePrice(p.BasePriceCents, productRule.PriceOverrideCents)
		}

		// 2. Branch modifier rules price overrides
		var optionIDs []string
		for _, g := range p.ModifierGroups {
			for _, opt := range g.Options {
				optionIDs = append(optionIDs, opt.ID)
			}
		}
		if len(optionIDs) > 0 {
			modRules, err := s.store.GetBranchModifierRulesByOptionIDs(ctx, resolvedBranchID, optionIDs)
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

	// For non-super-admin roles, storePrices must not be shown, but the caller's
	// visible branch scope should still be exposed through storeIds.
	p.StorePrices = nil
	p.BranchIds = s.visibleBranchIDsForClaims(ctx, claims, p.BranchIds)
	for gi := range p.ModifierGroups {
		for oi := range p.ModifierGroups[gi].Options {
			p.ModifierGroups[gi].Options[oi].StorePrices = nil
		}
	}

	return p, nil
}

func (s *Service) visibleBranchIDsForClaims(ctx context.Context, claims *auth.StaffClaims, allBranchIDs []string) []string {
	if claims == nil {
		return nil
	}

	if tenant.IsSuperAdmin(claims) || tenant.IsFinance(claims) {
		return allBranchIDs
	}

	visible := make(map[string]bool)
	for _, ra := range claims.RoleAssignments {
		if ra.ScopeType == tenant.ScopeBranch && ra.BranchID != nil && *ra.BranchID != "" {
			visible[*ra.BranchID] = true
		}
	}

	if len(visible) == 0 {
		return allBranchIDs
	}

	out := make([]string, 0, len(visible))
	for _, bid := range allBranchIDs {
		if visible[bid] {
			out = append(out, bid)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (s *Service) ResolveBranchIDForStaff(ctx context.Context, claims *auth.StaffClaims, targetBranchID ...string) string {
	if len(targetBranchID) > 0 && targetBranchID[0] != "" {
		reqBranch := targetBranchID[0]
		b, err := s.branches.GetBranch(ctx, reqBranch)
		if err == nil && b != nil && tenant.CanAccessBranch(claims, "", b.ID) {
			return b.ID
		}
	}

	if claims == nil {
		return ""
	}

	for _, ra := range claims.RoleAssignments {
		if ra.ScopeType == tenant.ScopeBranch && ra.BranchID != nil && *ra.BranchID != "" {
			return *ra.BranchID
		}
	}
	return ""
}

// ListProductsAdminPage is the paged form of ListProductsAdmin, for the
// console's product table. The unpaged one stays because other callers (the
// availability grid, the modifier picker) genuinely want the whole catalogue.
func (s *Service) ListProductsAdminPage(ctx context.Context, claims *auth.StaffClaims, categoryID, search string, showOnPos, showOnWebsite *bool, limit, offset int, targetBranchID ...string) ([]catalogapi.Product, int, error) {
	if !tenant.HasPermission(claims, tenant.PermCatalogueView) {
		return nil, 0, apperr.PermissionDenied("You do not have permission to view the catalogue.")
	}
	requested := ""
	if len(targetBranchID) > 0 {
		requested = targetBranchID[0]
	}
	// Which branches' products are listed (applied in SQL so the pager total
	// agrees), and — when the view is about one branch — whose prices apply.
	scopeBranchIDs, priceBranchID, err := s.adminBranchScope(ctx, claims, requested)
	if err != nil {
		return nil, 0, err
	}

	list, total, err := s.store.ListProductsAdminPage(ctx, store.AdminProductQuery{
		CategoryID: categoryID, Search: search, ShowOnPos: showOnPos, ShowOnWebsite: showOnWebsite,
		BranchIDs: scopeBranchIDs, Limit: limit, Offset: offset,
	})
	if err != nil {
		return nil, 0, apperr.Internal(err)
	}

	// One branch selected, by any role: price the list as that branch sells
	// it — its product override, and for variation-priced products its
	// modifier-option overrides, which is where their price actually lives.
	if priceBranchID != "" && len(list) > 0 {
		productIDs := make([]string, 0, len(list))
		for _, p := range list {
			productIDs = append(productIDs, p.ID)
		}
		rules, err := s.store.GetBranchProductRulesByProductIDs(ctx, priceBranchID, productIDs)
		if err == nil && len(rules) > 0 {
			for i := range list {
				if rule, ok := rules[list[i].ID]; ok {
					list[i].BasePriceCents = effectiveBranchBasePrice(list[i].BasePriceCents, rule.PriceOverrideCents)
				}
			}
		}
		s.applyBranchModifierOverridesToProducts(ctx, priceBranchID, list)
	}

	// 1. Super-admin and finance see all store prices across the network, or
	// only the selected branch's when filtering to one.
	if isHeadOffice(claims) {
		if priceBranchID != "" {
			only := map[string]bool{priceBranchID: true}
			for i := range list {
				list[i].StorePrices = filterProductStorePrices(list[i].StorePrices, only)
			}
		}
		return list, total, nil
	}

	// 2. Franchise owner / franchise viewer: store prices for their own
	// branches only (or the selected one).
	if tenant.HasRole(claims, tenant.RoleFranchiseOwner, tenant.RoleFranchiseViewer) {
		own := s.ownBranchSet(ctx, claims)
		if priceBranchID != "" {
			own = map[string]bool{priceBranchID: true}
		}
		for i := range list {
			list[i].StorePrices = filterProductStorePrices(list[i].StorePrices, own)
			list[i].BranchIds = s.visibleBranchIDsForClaims(ctx, claims, list[i].BranchIds)
		}
		return list, total, nil
	}

	// 3. Branch-scoped roles: no store prices, and storeIds trimmed to what
	// the caller can see.
	for i := range list {
		list[i].StorePrices = nil
		list[i].BranchIds = s.visibleBranchIDsForClaims(ctx, claims, list[i].BranchIds)
		for gi := range list[i].ModifierGroups {
			for oi := range list[i].ModifierGroups[gi].Options {
				list[i].ModifierGroups[gi].Options[oi].StorePrices = nil
			}
		}
	}

	return list, total, nil
}

func filterProductStorePrices(in []catalogapi.BranchProductRule, keep map[string]bool) []catalogapi.BranchProductRule {
	out := make([]catalogapi.BranchProductRule, 0, len(in))
	for _, sp := range in {
		if keep[sp.BranchID] {
			out = append(out, sp)
		}
	}
	return out
}

// applyBranchModifierOverridesToProducts is applyBranchModifierOverrides for
// bare products (the admin list has no availability wrapper).
func (s *Service) applyBranchModifierOverridesToProducts(ctx context.Context, branchID string, products []catalogapi.Product) {
	wrapped := make([]ProductWithAvailability, len(products))
	for i := range products {
		wrapped[i] = ProductWithAvailability{Product: products[i]}
	}
	s.applyBranchModifierOverrides(ctx, branchID, wrapped)
	for i := range products {
		products[i] = wrapped[i].Product
	}
}

func (s *Service) ListProductsAdmin(ctx context.Context, claims *auth.StaffClaims, categoryID string) ([]catalogapi.Product, error) {
	if !tenant.HasPermission(claims, tenant.PermCatalogueView) {
		return nil, apperr.PermissionDenied("You do not have permission to view the catalogue.")
	}
	list, err := s.store.ListProducts(ctx, categoryID, "", true)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return list, nil
}
