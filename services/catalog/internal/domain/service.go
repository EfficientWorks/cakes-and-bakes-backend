package domain

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/audit"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/strx"
	"bakehouse/pkg/tenant"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/catalog/internal/store"
	"bakehouse/services/franchise/franchiseapi"
)

// BranchLookup is the minimal read-only view of tenancy this module needs:
// a product's availability cascade is evaluated in the branch's own local
// timezone (CAT-04), and every catalogue write needs to know which
// franchise the target branch belongs to for permission scoping.
type BranchLookup interface {
	GetBranch(ctx context.Context, id string) (*franchiseapi.Branch, error)
}

type Service struct {
	store    *store.Store
	audit    *audit.Writer
	branches BranchLookup
}

func NewService(store *store.Store, auditWriter *audit.Writer, branches BranchLookup) *Service {
	return &Service{store: store, audit: auditWriter, branches: branches}
}

// --- Storefront: categories & product listing ---------------------------------

func (s *Service) ListCategories(ctx context.Context) ([]catalogapi.Category, error) {
	cats, err := s.store.ListCategories(ctx)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return cats, nil
}

// ListCategoriesForBranch returns the categories a store can sell from —
// the till's tab bar. Without a branch it falls back to the full list, the
// same as the storefront before a shopper picks a store.
func (s *Service) ListCategoriesForBranch(ctx context.Context, branchID string) ([]catalogapi.Category, error) {
	if branchID == "" {
		return s.ListCategories(ctx)
	}
	cats, err := s.store.ListCategoriesForBranch(ctx, branchID)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return cats, nil
}

func (s *Service) GetCategory(ctx context.Context, id string) (*catalogapi.Category, error) {
	cat, err := s.store.GetCategoryByID(ctx, id)
	if err != nil {
		return nil, notFoundOrInternal(err, "Category not found.")
	}
	return cat, nil
}

// ListProductsForStorefront returns every active product in the master
// catalogue with its availability at branchID on date resolved (CAT-07:
// unavailable items are returned, never omitted, with a reason).
func (s *Service) ListProductsForStorefront(ctx context.Context, branchID, categoryID, occasion string, date time.Time) ([]ProductWithAvailability, error) {
	page, err := s.SearchProductsForStorefront(ctx, branchID, catalogapi.ProductFilter{
		CategoryID: categoryID,
		Occasions:  nonEmpty([]string{occasion}),
		Limit:      catalogapi.MaxProductPageSize,
	}, date)
	if err != nil {
		return nil, err
	}
	return page.Products, nil
}

// ProductPage is one page of the storefront grid, with the totals and facet
// bounds the filter panel needs to render itself.
type ProductPage struct {
	Products []ProductWithAvailability
	Total    int
	Page     int
	Limit    int
}

// SearchProductsForStorefront applies the Shop Now filters and resolves each
// result's availability at the branch.
//
// Availability is deliberately resolved after filtering rather than before:
// an item a branch has sold out still appears in the grid, de-emphasised with
// a reason and its next available date (CAT-07). Filtering it out would make
// the result count jump around as the kitchen toggles items during service.
func (s *Service) SearchProductsForStorefront(ctx context.Context, branchID string, f catalogapi.ProductFilter, date time.Time) (*ProductPage, error) {
	f = f.Normalise()

	// The branch is looked up before searching: its price overrides feed the
	// price filter and sorts in SQL, and an unknown or malformed id must be a
	// 404 rather than a database error.
	var branch *franchiseapi.Branch
	if branchID != "" {
		b, err := s.branches.GetBranch(ctx, branchID)
		if err != nil {
			return nil, apperr.NotFound("Branch not found.")
		}
		branch = b
		f.PriceBranchID = branchID
	}

	products, total, err := s.store.SearchProducts(ctx, f)
	if err != nil {
		return nil, apperr.Internal(err)
	}

	// Browsing does not require a branch. A shopper can open the catalogue
	// before telling us where they are, and only commits to a branch at
	// checkout — where every line is re-priced and re-checked against that
	// branch's availability before payment. Listing without a branch is
	// therefore a display decision, not a correctness hole.
	if branchID == "" {
		out := make([]ProductWithAvailability, 0, len(products))
		for _, p := range products {
			out = append(out, ProductWithAvailability{Product: p, Availability: unresolvedAvailability()})
		}
		return &ProductPage{Products: out, Total: total, Page: f.Page, Limit: f.Limit}, nil
	}

	rules, err := s.store.ListBranchProductRules(ctx, branchID)
	if err != nil {
		return nil, apperr.Internal(err)
	}

	categoryRules, err := s.store.ListBranchCategoryRules(ctx, branchID)
	if err != nil {
		return nil, apperr.Internal(err)
	}

	localDate := toLocalDate(date, branch.Timezone)
	out := make([]ProductWithAvailability, 0, len(products))
	for _, p := range products {
		catRule, ok := categoryRules[p.CategoryID]
		if !ok || !catRule.BranchEnabled {
			reason := catalogapi.ReasonCategoryNotMapped
			if ok && catRule.Reason != nil && *catRule.Reason != "" {
				reason = *catRule.Reason
			}
			out = append(out, ProductWithAvailability{
				Product:      p,
				Availability: catalogapi.Availability{Available: false, Reason: reason},
			})
			continue
		}

		rule, ok := rules[p.ID]
		if !ok {
			// No record in branch_product_rules: defaults to disabled (unavailable).
			rule = catalogapi.BranchProductRule{
				BranchID:      branchID,
				ProductID:     p.ID,
				BranchEnabled: false,
				AvailableDays: catalogapi.DefaultAvailableDays,
			}
		}

		// resolveAvailability now checks BranchEnabled first, so a disabled branch
		// is always unavailable regardless of any date/day overrides.
		avail := resolveAvailability(p, rule, localDate)

		// Apply branch price override if configured for this branch. A zero value
		// means "no override" and must fall back to the product's master base price.
		if rule.BranchEnabled {
			p.BasePriceCents = effectiveBranchBasePrice(p.BasePriceCents, rule.PriceOverrideCents)
		}

		out = append(out, ProductWithAvailability{Product: p, Availability: avail})
	}

	s.applyBranchModifierOverrides(ctx, branchID, out)

	return &ProductPage{Products: out, Total: total, Page: f.Page, Limit: f.Limit}, nil
}

func (s *Service) applyBranchModifierOverrides(ctx context.Context, branchID string, products []ProductWithAvailability) {
	var optionIDs []string
	for i := range products {
		for _, g := range products[i].ModifierGroups {
			for _, opt := range g.Options {
				optionIDs = append(optionIDs, opt.ID)
			}
		}
	}
	if branchID == "" || len(optionIDs) == 0 {
		return
	}
	modRules, err := s.store.GetBranchModifierRulesByOptionIDs(ctx, branchID, optionIDs)
	if err != nil || len(modRules) == 0 {
		return
	}
	for i := range products {
		for gi := range products[i].ModifierGroups {
			for oi := range products[i].ModifierGroups[gi].Options {
				opt := &products[i].ModifierGroups[gi].Options[oi]
				if mr, ok := modRules[opt.ID]; ok && mr.PriceOverrideCents != nil && *mr.PriceOverrideCents != 0 {
					opt.PriceDeltaCents = *mr.PriceOverrideCents
				}
			}
		}
	}
}

// unresolvedAvailability is what a product reports when no branch was supplied.
// Available is true so the storefront renders the item normally; Reason names
// the caveat so a client can show "check availability at checkout" rather than
// implying a guarantee nobody made.
func unresolvedAvailability() catalogapi.Availability {
	return catalogapi.Availability{Available: true, Reason: catalogapi.ReasonBranchNotSelected}
}

// StorefrontFacets describes the filter panel: which chips to offer and where
// the price slider starts and ends.
type StorefrontFacets struct {
	Occasions     []string
	Tags          []catalogapi.SeasonalTag
	Dietary       []string
	MinPriceCents int64
	MaxPriceCents int64
}

// Facets returns only the values that appear on active products, so the panel
// never offers a chip that returns nothing.
func (s *Service) Facets(ctx context.Context) (*StorefrontFacets, error) {
	occasions, dietary, err := s.store.DistinctFacets(ctx)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	minCents, maxCents, err := s.store.PriceBounds(ctx)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	tags, err := s.store.ListTagsInUse(ctx)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return &StorefrontFacets{
		Occasions:     strx.OrEmptySlice(occasions),
		Tags:          tags,
		Dietary:       strx.OrEmptySlice(dietary),
		MinPriceCents: minCents,
		MaxPriceCents: maxCents,
	}, nil
}

// nonEmpty drops blank entries so a missing query parameter does not become a
// filter that matches nothing.
func nonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v != "" {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

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

// --- Branch availability control (CAT-05 to CAT-10) ---------------------------

// AvailabilityGridRow is one line of the admin availability grid — products
// as rows, day-of-week columns, plus today's resolved status.
type AvailabilityGridRow struct {
	ProductID      string
	ProductName    string
	BranchEnabled  bool
	AvailableDays  []string
	TodayAvailable bool
	SeasonalFrom   *string
	SeasonalUntil  *string
	PriceOverride  *int64
}

func (s *Service) GetAvailabilityGrid(ctx context.Context, claims *auth.StaffClaims, branchID string, date time.Time) ([]AvailabilityGridRow, error) {
	branch, err := s.checkBranchCatalogueAccess(ctx, claims, branchID, tenant.PermCatalogueView)
	if err != nil {
		return nil, err
	}
	products, err := s.store.ListProducts(ctx, "", "", false)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	rules, err := s.store.ListBranchProductRules(ctx, branchID)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	localDate := toLocalDate(date, branch.Timezone)

	rows := make([]AvailabilityGridRow, 0, len(products))
	for _, p := range products {
		rule, ok := rules[p.ID]
		if !ok {
			rule = catalogapi.BranchProductRule{BranchEnabled: false, AvailableDays: catalogapi.DefaultAvailableDays}
		}
		avail := resolveAvailability(p, rule, localDate)
		var from, until *string
		if rule.AvailableFrom != nil {
			v := rule.AvailableFrom.Format("2006-01-02")
			from = &v
		}
		if rule.AvailableUntil != nil {
			v := rule.AvailableUntil.Format("2006-01-02")
			until = &v
		}
		rows = append(rows, AvailabilityGridRow{
			ProductID: p.ID, ProductName: p.Name, BranchEnabled: rule.BranchEnabled,
			AvailableDays: rule.AvailableDays, TodayAvailable: avail.Available,
			SeasonalFrom: from, SeasonalUntil: until, PriceOverride: rule.PriceOverrideCents,
		})
	}
	return rows, nil
}

// AvailabilityPatch is the set of fields an admin may change in one call
// (PATCH .../availability).
type AvailabilityPatch struct {
	BranchEnabled      *bool
	Reason             string
	AvailableDays      []string
	PriceOverrideCents *int64
	AvailableFrom      *time.Time
	AvailableUntil     *time.Time
}

func (s *Service) UpdateAvailability(ctx context.Context, claims *auth.StaffClaims, branchID, productID string, patch AvailabilityPatch) (*catalogapi.BranchProductRule, error) {
	if _, err := s.checkBranchCatalogueAccess(ctx, claims, branchID, tenant.PermCatalogueAvailability); err != nil {
		return nil, err
	}

	if patch.PriceOverrideCents != nil {
		product, err := s.store.GetProductByID(ctx, productID)
		if err != nil {
			return nil, notFoundOrInternal(err, "Product not found.")
		}
		if err := checkPriceBand(product, *patch.PriceOverrideCents); err != nil {
			return nil, err
		}
	}

	before, _ := s.store.GetBranchProductRule(ctx, branchID, productID)
	out, err := s.store.UpsertBranchProductRulePatch(ctx, branchID, productID, func(r *catalogapi.BranchProductRule) {
		if patch.BranchEnabled != nil {
			r.BranchEnabled = *patch.BranchEnabled
		}
		if patch.AvailableDays != nil {
			r.AvailableDays = patch.AvailableDays
		}
		if patch.PriceOverrideCents != nil {
			r.PriceOverrideCents = patch.PriceOverrideCents
		}
		if patch.AvailableFrom != nil {
			r.AvailableFrom = patch.AvailableFrom
		}
		if patch.AvailableUntil != nil {
			r.AvailableUntil = patch.AvailableUntil
		}
	}, claims.StaffID, patch.Reason)
	if err != nil {
		return nil, apperr.Internal(err)
	}

	s.writeAudit(ctx, claims, "catalogue.availability.update", "branch_product_rule", &out.ID, &branchID, before, out)
	return out, nil
}

// checkPriceBand enforces GOV-10: a franchise price override outside the
// brand-defined floor/ceiling is rejected with an explanatory message.
func checkPriceBand(p *catalogapi.Product, priceCents int64) error {
	if p.PriceFloorCents != nil && priceCents < *p.PriceFloorCents {
		return apperr.New("PRICE_BELOW_FLOOR", apperr.CategoryBusinessRule,
			fmt.Sprintf("Price cannot be set below the brand floor of $%.2f for this product.", float64(*p.PriceFloorCents)/100))
	}
	if p.PriceCeilingCents != nil && priceCents > *p.PriceCeilingCents {
		return apperr.New("PRICE_ABOVE_CEILING", apperr.CategoryBusinessRule,
			fmt.Sprintf("Price cannot be set above the brand ceiling of $%.2f for this product.", float64(*p.PriceCeilingCents)/100))
	}
	return nil
}

func (s *Service) AddDateOverride(ctx context.Context, claims *auth.StaffClaims, branchID, productID string, override catalogapi.DateOverride) (*catalogapi.BranchProductRule, error) {
	if _, err := s.checkBranchCatalogueAccess(ctx, claims, branchID, tenant.PermCatalogueAvailability); err != nil {
		return nil, err
	}
	before, _ := s.store.GetBranchProductRule(ctx, branchID, productID)
	out, err := s.store.UpsertBranchProductRulePatch(ctx, branchID, productID, func(r *catalogapi.BranchProductRule) {
		replaced := false
		for i, o := range r.DateOverrides {
			if o.Date == override.Date {
				r.DateOverrides[i] = override
				replaced = true
				break
			}
		}
		if !replaced {
			r.DateOverrides = append(r.DateOverrides, override)
		}
	}, claims.StaffID, override.Reason)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	s.writeAudit(ctx, claims, "catalogue.date_override.create", "branch_product_rule", &out.ID, &branchID, before, out)
	return out, nil
}

func (s *Service) BulkDisable(ctx context.Context, claims *auth.StaffClaims, branchID string, productIDs []string, reason string) (int, error) {
	if _, err := s.checkBranchCatalogueAccess(ctx, claims, branchID, tenant.PermCatalogueAvailability); err != nil {
		return 0, err
	}
	if strings.TrimSpace(reason) == "" {
		return 0, apperr.Validation("A reason is required to bulk-disable items.")
	}
	count, err := s.store.BulkDisable(ctx, branchID, productIDs, reason, claims.StaffID)
	if err != nil {
		return 0, apperr.Internal(err)
	}
	s.writeAudit(ctx, claims, "catalogue.bulk_disable", "branch", &branchID, &branchID, nil, map[string]interface{}{"productIds": productIDs, "reason": reason})
	return count, nil
}

// SetSoldOut is the one-tap sold-out control (CAT-05, ADM-14): a date
// override for today, no navigation into settings required.
func (s *Service) SetSoldOut(ctx context.Context, claims *auth.StaffClaims, branchID, productID, reason string) error {
	branch, err := s.checkBranchCatalogueAccess(ctx, claims, branchID, tenant.PermCatalogueSoldOut)
	if err != nil {
		return err
	}
	today := toLocalDate(time.Now(), branch.Timezone).Format("2006-01-02")
	_, err = s.AddDateOverride(ctx, claims, branchID, productID, catalogapi.DateOverride{Date: today, Available: false, Reason: strx.OrDefault(reason, "Sold out for today")})
	return err
}

func (s *Service) RestoreToday(ctx context.Context, claims *auth.StaffClaims, branchID, productID string) error {
	branch, err := s.checkBranchCatalogueAccess(ctx, claims, branchID, tenant.PermCatalogueSoldOut)
	if err != nil {
		return err
	}
	today := toLocalDate(time.Now(), branch.Timezone).Format("2006-01-02")
	before, _ := s.store.GetBranchProductRule(ctx, branchID, productID)
	out, err := s.store.UpsertBranchProductRulePatch(ctx, branchID, productID, func(r *catalogapi.BranchProductRule) {
		filtered := r.DateOverrides[:0]
		for _, o := range r.DateOverrides {
			if o.Date != today {
				filtered = append(filtered, o)
			}
		}
		r.DateOverrides = filtered
	}, claims.StaffID, "")
	if err != nil {
		return apperr.Internal(err)
	}
	s.writeAudit(ctx, claims, "catalogue.restore", "branch_product_rule", &out.ID, &branchID, before, out)
	return nil
}

// checkBranchCatalogueAccess resolves the branch (for timezone + franchise)
// and enforces both the permission and the branch scope in one place.
func (s *Service) checkBranchCatalogueAccess(ctx context.Context, claims *auth.StaffClaims, branchID, permission string) (*franchiseapi.Branch, error) {
	if !tenant.HasPermission(claims, permission) {
		return nil, apperr.PermissionDenied("You do not have permission to manage this branch's catalogue.")
	}
	branch, err := s.branches.GetBranch(ctx, branchID)
	if err != nil {
		return nil, apperr.NotFound("Branch not found.")
	}
	if !tenant.CanAccessBranch(claims, "", branch.ID) {
		return nil, apperr.PermissionDenied("You do not have access to this branch.")
	}
	return branch, nil
}

// --- Site content (SF-35, SA-12) ----------------------------------------------

func (s *Service) GetContent(ctx context.Context, keys []string) (map[string]string, error) {
	items, err := s.store.GetContent(ctx, keys)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	out := make(map[string]string, len(items))
	for _, i := range items {
		out[i.Key] = i.Value
	}
	return out, nil
}

func (s *Service) GetContentByKey(ctx context.Context, key string) (*catalogapi.ContentItem, error) {
	item, err := s.store.GetContentByKey(ctx, key)
	if err != nil {
		return nil, apperr.Internal(err)
	}

	return item, nil
}

func (s *Service) SetContent(ctx context.Context, claims *auth.StaffClaims, key, value, contentType string) error {
	if !tenant.HasPermission(claims, tenant.PermCatalogueEditMaster) {
		return apperr.PermissionDenied("Only super admin can edit site content.")
	}
	if err := s.store.SetContent(ctx, key, value, contentType, claims.StaffID); err != nil {
		return apperr.Internal(err)
	}
	s.writeAudit(ctx, claims, "content.update", "site_content", &key, nil, nil, map[string]string{"key": key, "value": value})
	return nil
}

func notFoundOrInternal(err error, msg string) error {
	if errors.Is(err, store.ErrNotFound) {
		return apperr.NotFound(msg)
	}
	return apperr.Internal(err)
}

func (s *Service) writeAudit(ctx context.Context, claims *auth.StaffClaims, action, resourceType string, resourceID, branchID *string, before, after interface{}) {
	if s.audit == nil || claims == nil {
		return
	}
	_ = s.audit.Write(ctx, audit.Entry{
		ActorID: &claims.StaffID, ActorType: "staff", Action: action,
		ResourceType: resourceType, ResourceID: resourceID, BranchID: branchID,
		BeforeValue: before, AfterValue: after,
	})
}
