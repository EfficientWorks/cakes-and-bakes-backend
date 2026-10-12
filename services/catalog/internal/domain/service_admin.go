package domain

import (
	"context"
	"errors"
	"strings"
	"time"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/tenant"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/catalog/internal/store"
)

// Admin-console catalogue operations: categories, the modifier library, and
// coupons.
//
// Governance runs through every method here (GOV-02). Categories and modifiers
// are brand-level — they define what the catalogue *is*, so only the brand may
// edit them. Coupons split: a network-wide coupon is brand-level (SA-13), a
// branch-scoped one is a franchise's local promotion (ADM-23). Each check is
// server-side; the console hides what a user cannot do, but hiding a control
// is not enforcing a rule (RBAC-08).

// --- Categories --------------------------------------------------------------

// adminBranchScope decides which branches' slice of the catalogue a console
// view covers. requested is the branchId/storeId the screen asked for.
//
//   - super admin / finance: the whole master catalogue (nil), or exactly the
//     requested branch when one is given (404 if it does not exist).
//   - franchise owner / viewer: everything mapped to any of their branches, or
//     the requested branch if it is theirs (a branch that is not theirs
//     narrows nothing — the filter can narrow, never widen).
//   - branch roles (branch manager, counter, kitchen): only their branch —
//     the requested one if they hold it, else their own.
//
// priceBranchID is set whenever the view is about exactly one branch, and is
// the branch whose price overrides apply. A non-nil, empty branchIDs means
// "nothing": a branch login with no branch must not fall through to the full
// catalogue.
func (s *Service) adminBranchScope(ctx context.Context, claims *auth.StaffClaims, requested string) (branchIDs []string, priceBranchID string, err error) {
	if claims == nil || tenant.IsSuperAdmin(claims) || tenant.IsFinance(claims) {
		if requested == "" {
			return nil, "", nil
		}
		if _, err := s.branches.GetBranch(ctx, requested); err != nil {
			return nil, "", apperr.NotFound("Branch not found.")
		}
		return []string{requested}, requested, nil
	}

	if tenant.HasRole(claims, tenant.RoleFranchiseOwner, tenant.RoleFranchiseViewer) {
		own := s.ownBranchSet(ctx, claims)
		if requested != "" && own[requested] {
			return []string{requested}, requested, nil
		}
		out := make([]string, 0, len(own))
		for id := range own {
			out = append(out, id)
		}
		return out, "", nil
	}

	branchID := s.ResolveBranchIDForStaff(ctx, claims, requested)
	if branchID == "" {
		return []string{}, "", nil
	}
	return []string{branchID}, branchID, nil
}

// ownBranchSet is every branch assigned to the caller directly.
func (s *Service) ownBranchSet(_ context.Context, claims *auth.StaffClaims) map[string]bool {
	own := map[string]bool{}
	for _, ra := range claims.RoleAssignments {
		if ra.BranchID != nil && *ra.BranchID != "" {
			own[*ra.BranchID] = true
		}
	}
	return own
}

// isHeadOffice is super admin or finance — the roles that see every store's
// prices and every branch in storeIds.
func isHeadOffice(claims *auth.StaffClaims) bool {
	return tenant.IsSuperAdmin(claims) || tenant.IsFinance(claims)
}

func (s *Service) ListCategoriesAdmin(ctx context.Context, claims *auth.StaffClaims, targetBranchID ...string) ([]store.CategoryRow, error) {
	if !tenant.HasPermission(claims, tenant.PermCatalogueView) {
		return nil, apperr.PermissionDenied("You do not have permission to view the catalogue.")
	}
	branchIDs, _, err := s.adminBranchScope(ctx, claims, firstOr(targetBranchID))
	if err != nil {
		return nil, err
	}

	rows, err := s.store.ListCategoriesAdmin(ctx, branchIDs)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	// storeIds on each row is trimmed to the branches the caller can see, so a
	// branch login is never shown which other outlets carry a category. Head
	// office keeps the full list even when filtering to one branch.
	if branchIDs != nil && !isHeadOffice(claims) {
		for i := range rows {
			rows[i].BranchIds = intersectIDs(rows[i].BranchIds, branchIDs)
		}
	}
	return rows, nil
}

func (s *Service) GetCategoryAdmin(ctx context.Context, claims *auth.StaffClaims, id string, targetBranchID ...string) (*store.CategoryRow, error) {
	if !tenant.HasPermission(claims, tenant.PermCatalogueView) {
		return nil, apperr.PermissionDenied("You do not have permission to view the catalogue.")
	}
	row, err := s.store.GetCategoryAdmin(ctx, id)
	if err != nil {
		return nil, notFoundOrInternal(err, "Category not found.")
	}
	if isHeadOffice(claims) {
		return row, nil
	}
	branchIDs, _, err := s.adminBranchScope(ctx, claims, firstOr(targetBranchID))
	if err != nil {
		return nil, err
	}
	if branchIDs != nil {
		row.BranchIds = intersectIDs(row.BranchIds, branchIDs)
		if len(row.BranchIds) == 0 {
			return nil, apperr.NotFound("Category not found.")
		}
	}
	return row, nil
}

func firstOr(v []string) string {
	if len(v) > 0 {
		return v[0]
	}
	return ""
}

// intersectIDs keeps the entries of ids that also appear in allowed, in the
// order of ids. Always returns a non-nil slice.
func intersectIDs(ids, allowed []string) []string {
	set := make(map[string]bool, len(allowed))
	for _, a := range allowed {
		set[a] = true
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if set[id] {
			out = append(out, id)
		}
	}
	return out
}

func (s *Service) SaveCategory(ctx context.Context, claims *auth.StaffClaims, id string, w catalogapi.CategoryWrite) (*store.CategoryRow, error) {
	if !tenant.HasPermission(claims, tenant.PermCatalogueEditMaster) {
		return nil, apperr.PermissionDenied("Only super admin can edit the master catalogue.")
	}
	if strings.TrimSpace(w.Name) == "" {
		return nil, apperr.Validation("Category name is required.").
			WithFields(apperr.FieldError{Field: "name", Code: "REQUIRED", Message: "Give the category a name."})
	}
	if w.Slug == "" {
		w.Slug = slugify(w.Name)
	}
	// A window that ends before it starts would silently hide the category
	// for good, which is not what anyone means by scheduling it.
	if w.AvailableFrom != nil && w.AvailableUntil != nil && w.AvailableUntil.Before(*w.AvailableFrom) {
		return nil, apperr.Validation("The end date cannot fall before the start date.").
			WithFields(apperr.FieldError{Field: "availableUntil", Code: "INVALID", Message: "Must be on or after the start date."})
	}

	var (
		out *store.CategoryRow
		err error
	)
	if id == "" {
		out, err = s.store.CreateCategory(ctx, w)
	} else {
		out, err = s.store.UpdateCategory(ctx, id, w)
	}
	if err != nil {
		return nil, categoryWriteError(err, w)
	}

	action := "category.create"
	if id != "" {
		action = "category.update"
	}
	s.writeAudit(ctx, claims, action, "category", &out.Category.ID, nil, nil, out.Category)
	return out, nil
}

// categoryWriteError turns a failed category save into something the person
// filling the form can act on. The slug is the only unique column, and it is
// derived from the name when none is sent, so "already exists" almost always
// means the name is taken.
func categoryWriteError(err error, w catalogapi.CategoryWrite) error {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return apperr.NotFound("Category not found.")
	case errors.Is(err, store.ErrDuplicate):
		return apperr.New("CATEGORY_ALREADY_EXISTS", apperr.CategoryConflict,
			"A category named \""+strings.TrimSpace(w.Name)+"\" already exists (web address \""+w.Slug+"\"). Use a different name or slug.").
			WithFields(
				apperr.FieldError{Field: "name", Code: "TAKEN", Message: "A category with this name already exists."},
				apperr.FieldError{Field: "slug", Code: "TAKEN", Message: "Already in use."},
			)
	case errors.Is(err, store.ErrRelatedMissing):
		return apperr.Validation("One of the selected stores no longer exists. Refresh and choose again.").
			WithFields(apperr.FieldError{Field: "storeIds", Code: "NOT_FOUND", Message: "Unknown store."})
	case errors.Is(err, store.ErrMalformedID):
		return apperr.Validation("One of the selected stores is not valid. Choose from the list.").
			WithFields(apperr.FieldError{Field: "storeIds", Code: "INVALID", Message: "Invalid store id."})
	default:
		return apperr.Internal(err)
	}
}

// SetCategoryActive is CAT-05 at category scope: instant, no approval step.
func (s *Service) SetCategoryActive(ctx context.Context, claims *auth.StaffClaims, id string, isActive bool) error {
	if !tenant.HasPermission(claims, tenant.PermCatalogueEditMaster) {
		return apperr.PermissionDenied("Only super admin can enable or disable a category.")
	}
	if err := s.store.SetCategoryActive(ctx, id, isActive); err != nil {
		return notFoundOrInternal(err, "Category not found.")
	}
	s.writeAudit(ctx, claims, "category.availability", "category", &id, nil, nil,
		map[string]any{"isActive": isActive})
	return nil
}

func (s *Service) DeleteCategory(ctx context.Context, claims *auth.StaffClaims, id string) error {
	if !tenant.HasPermission(claims, tenant.PermCatalogueEditMaster) {
		return apperr.PermissionDenied("Only super admin can delete a category.")
	}
	// Products reference their category with a NOT NULL foreign key, so
	// deleting one that still holds products would fail at the database with
	// an opaque error. Refusing here says what is actually wrong.
	live, total, err := s.store.CountProductsInCategory(ctx, id)
	if err != nil {
		return apperr.Internal(err)
	}
	if live > 0 {
		return apperr.New("CATEGORY_NOT_EMPTY", apperr.CategoryBusinessRule,
			"This category still holds products. Move them first, or turn the category off to hide it from customers.")
	}
	if total > 0 {
		// Deleted products still point at their category, so the row cannot go.
		// Saying so is better than the constraint violation this used to become.
		return apperr.New("CATEGORY_NOT_EMPTY", apperr.CategoryBusinessRule,
			"This category cannot be removed because deleted products still refer to it. Turn it off instead — customers will not see it.")
	}
	if err := s.store.DeleteCategory(ctx, id); err != nil {
		return notFoundOrInternal(err, "Category not found.")
	}
	s.writeAudit(ctx, claims, "category.delete", "category", &id, nil, nil, nil)
	return nil
}

// --- Modifier library --------------------------------------------------------

// ListModifierGroups is the modifier library.
//
// When the view is about one branch — a branch login, or anyone who asked for
// a branchId they hold — it lists only the groups used by that branch's
// products, with option prices as that branch charges them. Otherwise it is
// the whole library, with store prices trimmed to what the caller may see.
func (s *Service) ListModifierGroups(ctx context.Context, claims *auth.StaffClaims, targetBranchID ...string) ([]catalogapi.ModifierGroup, error) {
	if !tenant.HasPermission(claims, tenant.PermCatalogueView) {
		return nil, apperr.PermissionDenied("You do not have permission to view the catalogue.")
	}
	_, branchID, err := s.adminBranchScope(ctx, claims, firstOr(targetBranchID))
	if err != nil {
		return nil, err
	}
	groups, err := s.store.ListModifierGroups(ctx)
	if err != nil {
		return nil, apperr.Internal(err)
	}

	if branchID != "" {
		used, err := s.store.ModifierGroupIDsForBranches(ctx, []string{branchID})
		if err != nil {
			return nil, apperr.Internal(err)
		}
		kept := make([]catalogapi.ModifierGroup, 0, len(groups))
		for _, g := range groups {
			if used[g.ID] {
				kept = append(kept, g)
			}
		}
		groups = kept
		s.applyBranchOptionPrices(ctx, branchID, groups)
	}

	// Which store prices each option shows.
	var keep map[string]bool // nil = all
	switch {
	case isHeadOffice(claims):
		if branchID != "" {
			keep = map[string]bool{branchID: true}
		}
	case tenant.HasRole(claims, tenant.RoleFranchiseOwner, tenant.RoleFranchiseViewer):
		keep = s.ownBranchSet(ctx, claims)
		if branchID != "" {
			keep = map[string]bool{branchID: true}
		}
	default:
		keep = map[string]bool{} // branch roles never see store prices
	}
	if keep != nil {
		for gi := range groups {
			for oi := range groups[gi].Options {
				opt := &groups[gi].Options[oi]
				if len(keep) == 0 {
					opt.StorePrices = nil
					continue
				}
				filtered := make([]catalogapi.BranchModifierRule, 0, len(opt.StorePrices))
				for _, sp := range opt.StorePrices {
					if keep[sp.BranchID] {
						filtered = append(filtered, sp)
					}
				}
				opt.StorePrices = filtered
			}
		}
	}
	return groups, nil
}

// applyBranchOptionPrices sets each option's price to the branch's override
// where one is set (zero means "no override").
func (s *Service) applyBranchOptionPrices(ctx context.Context, branchID string, groups []catalogapi.ModifierGroup) {
	var optionIDs []string
	for _, g := range groups {
		for _, opt := range g.Options {
			optionIDs = append(optionIDs, opt.ID)
		}
	}
	if len(optionIDs) == 0 {
		return
	}
	modRules, err := s.store.GetBranchModifierRulesByOptionIDs(ctx, branchID, optionIDs)
	if err != nil || len(modRules) == 0 {
		return
	}
	for gi := range groups {
		for oi := range groups[gi].Options {
			opt := &groups[gi].Options[oi]
			if mr, ok := modRules[opt.ID]; ok && mr.PriceOverrideCents != nil && *mr.PriceOverrideCents != 0 {
				opt.PriceDeltaCents = *mr.PriceOverrideCents
			}
		}
	}
}

func (s *Service) SaveModifierGroup(ctx context.Context, claims *auth.StaffClaims, g catalogapi.ModifierGroup) (string, error) {
	if !tenant.HasPermission(claims, tenant.PermCatalogueEditMaster) {
		return "", apperr.PermissionDenied("Only super admin can edit modifiers.")
	}
	if strings.TrimSpace(g.DisplayName) == "" {
		return "", apperr.Validation("Display name is required — it is what the customer reads.").
			WithFields(apperr.FieldError{Field: "displayName", Code: "REQUIRED", Message: "Required."})
	}
	if strings.TrimSpace(g.Name) == "" {
		g.Name = g.DisplayName
	}
	named := 0
	for _, o := range g.Options {
		if strings.TrimSpace(o.Name) != "" {
			named++
		}
	}

	/*
	 * A group with no options yet is allowed on create, and only on create.
	 *
	 * A catalogue arriving as a spreadsheet names its add-on and size sets on
	 * the product rows, and does not always describe them anywhere. Refusing
	 * those would mean dropping the product's reference to a set the client
	 * clearly intends to have — so the set is created empty, attached, and
	 * finished in the console.
	 *
	 * It is inert until then: an empty group is never required, so it cannot
	 * block a customer from ordering, and the storefront has nothing to draw.
	 * Editing an existing group down to nothing is still refused, because that
	 * is somebody removing options from a group already on sale.
	 */
	if named == 0 {
		if g.ID != "" {
			return "", apperr.Validation("Add at least one option.").
				WithFields(apperr.FieldError{Field: "options", Code: "REQUIRED", Message: "Add at least one option."})
		}
		g.IsRequired = false
		g.MinSelect = 0
	}

	if g.Kind != catalogapi.ModifierKindVariation {
		g.Kind = catalogapi.ModifierKindAddon
	}
	// A variation is one choice from a set, so its bounds are fixed by what
	// it is rather than by what the form sent.
	if g.Kind == catalogapi.ModifierKindVariation {
		g.AllowMultiple = false
		g.MaxSelect = 1
		g.MinSelect = 0
		if g.IsRequired {
			g.MinSelect = 1
		}
		// At most one pre-selected option: it is what the product page loads
		// with, and two defaults is not a state the storefront can render.
		seen := false
		for i := range g.Options {
			if g.Options[i].IsPreSelected {
				if seen {
					g.Options[i].IsPreSelected = false
				}
				seen = true
			}
		}
	} else {
		for i := range g.Options {
			g.Options[i].IsPreSelected = false
		}
		if !g.AllowMultiple {
			g.MaxSelect = 1
		}
		if g.MaxSelect > 0 && g.MaxSelect < g.MinSelect {
			return "", apperr.Validation("The maximum cannot be lower than the minimum.").
				WithFields(apperr.FieldError{Field: "maxSelect", Code: "INVALID", Message: "Must be at least the minimum."})
		}
	}
	if g.Type == "" {
		// The finer storefront taxonomy still has to be set, since the
		// product page groups by it.
		if g.Kind == catalogapi.ModifierKindVariation {
			g.Type = catalogapi.ModifierPresentation
		} else {
			g.Type = catalogapi.ModifierAddon
		}
	}

	id, err := s.store.SaveModifierGroup(ctx, g)
	if err != nil {
		return "", notFoundOrInternal(err, "Modifier not found.")
	}
	action := "modifier.create"
	if g.ID != "" {
		action = "modifier.update"
	}
	s.writeAudit(ctx, claims, action, "modifier_group", &id, nil, nil, g)
	return id, nil
}

func (s *Service) DeleteModifierGroup(ctx context.Context, claims *auth.StaffClaims, id string) error {
	if !tenant.HasPermission(claims, tenant.PermCatalogueEditMaster) {
		return apperr.PermissionDenied("Only super admin can delete modifiers.")
	}
	if err := s.store.DeleteModifierGroup(ctx, id); err != nil {
		return notFoundOrInternal(err, "Modifier not found.")
	}
	s.writeAudit(ctx, claims, "modifier.delete", "modifier_group", &id, nil, nil, nil)
	return nil
}

// ApplyModifierToProducts is the modifier library's "apply to items".
//
// Both kinds attach the same way, for the same reason as above: what makes a
// variation different is how the storefront draws it, not where it is kept.
func (s *Service) ApplyModifierToProducts(ctx context.Context, claims *auth.StaffClaims, id string, productIDs []string) error {
	if !tenant.HasPermission(claims, tenant.PermCatalogueEditMaster) {
		return apperr.PermissionDenied("Only super admin can apply modifiers to products.")
	}
	if err := s.store.ApplyModifierToProducts(ctx, id, productIDs); err != nil {
		return apperr.Internal(err)
	}
	s.writeAudit(ctx, claims, "modifier.apply", "modifier_group", &id, nil, nil,
		map[string]any{"productIds": productIDs})
	return nil
}

// --- Coupons -----------------------------------------------------------------

// ListCoupons lists the coupon admin.
//
// Head office sees every coupon. A franchise or branch user sees network-wide
// coupons (they are live at their outlets) plus their own branches' — never
// another branch's local promotion. With targetBranchID, the list is what is
// live at that one branch: network-wide coupons plus that branch's own; the
// branch must be one the caller holds.
func (s *Service) ListCoupons(ctx context.Context, claims *auth.StaffClaims, targetBranchID ...string) ([]catalogapi.Coupon, error) {
	if !tenant.HasPermission(claims, tenant.PermCatalogueView) {
		return nil, apperr.PermissionDenied("You do not have permission to view coupons.")
	}
	branchID := firstOr(targetBranchID)
	if branchID != "" {
		b, err := s.branches.GetBranch(ctx, branchID)
		if err != nil {
			return nil, apperr.NotFound("Branch not found.")
		}
		if !tenant.IsBrandScoped(claims) && !tenant.CanAccessBranch(claims, "", b.ID) {
			return nil, apperr.PermissionDenied("You do not have access to this branch.")
		}
	}

	coupons, err := s.store.ListCoupons(ctx)
	if err != nil {
		return nil, apperr.Internal(err)
	}

	visible := make([]catalogapi.Coupon, 0, len(coupons))
	for _, c := range coupons {
		if c.BranchID == nil {
			visible = append(visible, c) // network-wide: live everywhere
			continue
		}
		if branchID != "" {
			if *c.BranchID == branchID {
				visible = append(visible, c)
			}
			continue
		}
		if tenant.IsBrandScoped(claims) {
			visible = append(visible, c)
			continue
		}
		if tenant.CanAccessBranch(claims, "", *c.BranchID) {
			visible = append(visible, c)
		}
	}
	return visible, nil
}

func (s *Service) SaveCoupon(ctx context.Context, claims *auth.StaffClaims, id string, c catalogapi.Coupon) (*catalogapi.Coupon, error) {
	// The governance level follows the scope. A network-wide coupon changes
	// pricing at every outlet, so only the brand may create one; a
	// branch-scoped coupon is that branch's own promotion.
	if c.BranchID == nil {
		if !tenant.IsBrandScoped(claims) {
			return nil, apperr.PermissionDenied("Only head office can run a network-wide promotion. Choose an outlet to run a local one.")
		}
	} else if !tenant.CanAccessBranch(claims, "", *c.BranchID) {
		return nil, apperr.PermissionDenied("You do not have access to that outlet.")
	}

	if strings.TrimSpace(c.Name) == "" {
		return nil, apperr.Validation("Coupon name is required.").
			WithFields(apperr.FieldError{Field: "name", Code: "REQUIRED", Message: "Give the coupon a name."})
	}
	if strings.TrimSpace(c.Code) == "" {
		return nil, apperr.Validation("Customers need a code to type.").
			WithFields(apperr.FieldError{Field: "code", Code: "REQUIRED", Message: "Required."})
	}
	switch c.Type {
	case "percentage":
		if c.DiscountValue <= 0 || c.DiscountValue > 100 {
			return nil, apperr.Validation("A percentage discount must be between 1 and 100.").
				WithFields(apperr.FieldError{Field: "discountValue", Code: "INVALID", Message: "Between 1 and 100."})
		}
	case "fixed":
		if c.DiscountValue <= 0 {
			return nil, apperr.Validation("Enter a discount greater than zero.").
				WithFields(apperr.FieldError{Field: "discountValue", Code: "INVALID", Message: "Must be above zero."})
		}
	case "free_shipping", "free_delivery":
		c.DiscountValue = 0
	default:
		return nil, apperr.Validation("Unknown coupon type.").
			WithFields(apperr.FieldError{Field: "type", Code: "INVALID", Message: "percentage, fixed, free_delivery or free_shipping."})
	}
	// appliesTo is who or what the code is for: the whole basket, chosen
	// products or categories, or a new customer's first order. The console
	// sends the singular spellings too.
	switch c.AppliesTo {
	case "", catalogapi.CouponAppliesAll:
		c.AppliesTo = catalogapi.CouponAppliesAll
	case "product", catalogapi.CouponAppliesProducts:
		c.AppliesTo = catalogapi.CouponAppliesProducts
	case "category", catalogapi.CouponAppliesCategories:
		c.AppliesTo = catalogapi.CouponAppliesCategories
	case catalogapi.CouponAppliesNewCustomer:
		// A first-order code covers the whole basket and runs until it is
		// paused or replaced: no targets, no window.
		c.TargetIDs = nil
		c.HasActiveDates = false
		c.StartsAt, c.EndsAt = nil, nil
	default:
		return nil, apperr.Validation("appliesTo must be all, products, categories or new_customer.").
			WithFields(apperr.FieldError{Field: "appliesTo", Code: "INVALID", Message: "all, products, categories or new_customer."})
	}
	if (c.AppliesTo == catalogapi.CouponAppliesProducts || c.AppliesTo == catalogapi.CouponAppliesCategories) && len(c.TargetIDs) == 0 {
		return nil, apperr.Validation("Choose at least one " + c.AppliesTo + ".").
			WithFields(apperr.FieldError{Field: "targetIds", Code: "REQUIRED", Message: "Choose at least one."})
	}
	if c.HasActiveDates {
		if c.StartsAt == nil || c.EndsAt == nil {
			return nil, apperr.Validation("Set both the start and end dates, or turn active dates off.")
		}
		if c.EndsAt.Before(*c.StartsAt) {
			return nil, apperr.Validation("The end date cannot fall before the start date.").
				WithFields(apperr.FieldError{Field: "endDate", Code: "INVALID", Message: "Must be on or after the start date."})
		}
	}
	// The status has to be checked here, not only in SetCouponStatus: the
	// column carries a CHECK constraint, so an unrecognised value used to
	// reach Postgres and surface as an internal error rather than as the
	// validation failure it actually is.
	switch c.Status {
	case "":
		c.Status = "active"
	case "active", "scheduled", "paused", "expired":
	default:
		return nil, apperr.Validation("Unknown coupon status.").
			WithFields(apperr.FieldError{Field: "status", Code: "INVALID",
				Message: "Must be active, scheduled, paused or expired."})
	}

	// One live new-customer code at a time. Checked here so the operator
	// gets a sentence rather than the unique index's error.
	if c.IsNewCustomer() && c.Status == catalogapi.CouponActive {
		if err := s.assertNoOtherActiveNewCustomerCoupon(ctx, id); err != nil {
			return nil, err
		}
	}

	var (
		out *catalogapi.Coupon
		err error
	)
	if id == "" {
		out, err = s.store.CreateCoupon(ctx, c, claims.StaffID)
	} else {
		out, err = s.store.UpdateCoupon(ctx, id, c)
	}
	if err != nil {
		// A duplicate code is the one failure an operator can act on, so it
		// gets its own message rather than a generic internal error.
		if isUniqueViolation(err) {
			if c.IsNewCustomer() && strings.Contains(err.Error(), "idx_coupons_one_active_new_customer") {
				return nil, errNewCustomerCouponExists
			}
			return nil, apperr.New("COUPON_CODE_IN_USE", apperr.CategoryConflict,
				"That code is already in use. Choose another.")
		}
		return nil, notFoundOrInternal(err, "Coupon not found.")
	}

	action := "coupon.create"
	if id != "" {
		action = "coupon.update"
	}
	s.writeAudit(ctx, claims, action, "coupon", &out.ID, nil, nil, out)
	return out, nil
}

func (s *Service) SetCouponStatus(ctx context.Context, claims *auth.StaffClaims, id, status string) error {
	if !tenant.HasPermission(claims, tenant.PermCatalogueView) {
		return apperr.PermissionDenied("You do not have permission to change coupons.")
	}
	switch status {
	case "active", "scheduled", "paused", "expired":
	default:
		return apperr.Validation("Unknown coupon status.")
	}
	// Reactivating a new-customer coupon is subject to the same one-at-a-time
	// rule as creating one.
	if status == catalogapi.CouponActive {
		existing, err := s.store.GetCoupon(ctx, id)
		if err != nil {
			return notFoundOrInternal(err, "Coupon not found.")
		}
		if existing.IsNewCustomer() {
			if err := s.assertNoOtherActiveNewCustomerCoupon(ctx, id); err != nil {
				return err
			}
		}
	}
	if err := s.store.SetCouponStatus(ctx, id, status); err != nil {
		if isUniqueViolation(err) {
			return errNewCustomerCouponExists
		}
		return notFoundOrInternal(err, "Coupon not found.")
	}
	s.writeAudit(ctx, claims, "coupon.status", "coupon", &id, nil, nil, map[string]any{"status": status})
	return nil
}

var errNewCustomerCouponExists = apperr.New("NEW_CUSTOMER_COUPON_EXISTS", apperr.CategoryConflict,
	"A new-customer coupon is already active. Pause or expire it before activating another one.")

// assertNoOtherActiveNewCustomerCoupon refuses a second live new-customer
// coupon. selfID is the coupon being saved, so updating the one that is
// already active does not trip over itself.
func (s *Service) assertNoOtherActiveNewCustomerCoupon(ctx context.Context, selfID string) error {
	activeID, err := s.store.ActiveNewCustomerCouponID(ctx)
	if err != nil {
		return apperr.Internal(err)
	}
	if activeID != "" && activeID != selfID {
		return errNewCustomerCouponExists
	}
	return nil
}

func (s *Service) DeleteCoupon(ctx context.Context, claims *auth.StaffClaims, id string) error {
	if !tenant.HasPermission(claims, tenant.PermCatalogueView) {
		return apperr.PermissionDenied("You do not have permission to delete coupons.")
	}
	if err := s.store.DeleteCoupon(ctx, id); err != nil {
		return notFoundOrInternal(err, "Coupon not found.")
	}
	s.writeAudit(ctx, claims, "coupon.delete", "coupon", &id, nil, nil, nil)
	return nil
}

// --- helpers -----------------------------------------------------------------

// slugify makes a URL-safe slug from a display name, so an operator never has
// to think about one.
func slugify(name string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		case !prevDash && b.Len() > 0:
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// isUniqueViolation recognises Postgres 23505 without importing the driver's
// error type into the domain layer.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "23505")
}

// dateOrNil parses a YYYY-MM-DD string into a time, treating empty as absent.
func dateOrNil(value string) *time.Time {
	if value == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil
	}
	return &t
}

// SetProductModifierGroups applies a set of library modifiers to one product —
// the product form's Variations and Add-Ons panels, which send one flat list.
//
// Both kinds are stored the same way: a link from the product to the group.
// They are different things to a customer — an add-on is an optional extra
// chosen by sight, a variation is the one priced choice that sets the price —
// but that is a difference in how they are drawn, not in where they live, and
// the storefront makes it from the group's kind.
//
// Storing a variation as the product's own price ladder instead meant a
// product could hold exactly one set of sizes, because the second replaced
// the first. A product that sells in two dimensions — a size and a flavour
// count, say — could not be expressed at all.
func (s *Service) SetProductModifierGroups(ctx context.Context, claims *auth.StaffClaims, productID string, groupIDs []string) error {
	if !tenant.HasPermission(claims, tenant.PermCatalogueEditMaster) {
		return apperr.PermissionDenied("Only super admin can change a product's modifiers.")
	}
	if _, err := s.store.GetProductByID(ctx, productID); err != nil {
		return notFoundOrInternal(err, "Product not found.")
	}
	if err := s.store.SetProductModifierGroups(ctx, productID, groupIDs); err != nil {
		return apperr.Internal(err)
	}
	s.writeAudit(ctx, claims, "product.modifiers", "product", &productID, nil, nil,
		map[string]any{"modifierGroupIds": groupIDs})
	return nil
}
