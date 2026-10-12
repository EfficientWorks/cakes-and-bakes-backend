package domain

import (
	"context"
	"errors"
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
