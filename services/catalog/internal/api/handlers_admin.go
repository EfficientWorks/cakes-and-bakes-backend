package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/httpx"
	"bakehouse/pkg/tenant"
	"bakehouse/services/catalog/catalogapi"
)

// Admin-console catalogue endpoints: category CRUD, the reusable modifier
// library, and coupons. Each backs a screen in backed-bouquet-admin.

// --- Categories --------------------------------------------------------------

// requestedBranchID reads the branch a console screen asked to be scoped to:
// ?branchId= or ?storeId=.
//
// It is a request, not an authority: the service only honours it when the
// caller actually holds that branch. Branch- and franchise-level logins may
// also carry it in X-Branch-ID / X-Store-ID, as the till does. Head office
// (super admin, finance) is only narrowed by an explicit query parameter, so
// a console that sends the header on every call does not silently turn the
// network-wide lists into one store's.
func requestedBranchID(r *http.Request) string {
	q := r.URL.Query()
	for _, v := range []string{q.Get("branchId"), q.Get("storeId")} {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	if c := staffClaims(r); c != nil && (tenant.IsSuperAdmin(c) || tenant.IsFinance(c)) {
		return ""
	}
	for _, v := range []string{r.Header.Get("X-Branch-ID"), r.Header.Get("X-Store-ID")} {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

type categoryAdminDTOType struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Slug           string   `json:"slug"`
	Description    *string  `json:"description"`
	ImageURL       *string  `json:"imageUrl"`
	ImageAltText   *string  `json:"imageAltText,omitempty"`
	ColorCode      *string  `json:"colorCode"`
	SortOrder      int      `json:"sortOrder"`
	IsActive       bool     `json:"isActive"`
	AvailableFrom  *string  `json:"availableFrom"`
	AvailableUntil *string  `json:"availableUntil"`
	BranchIds      []string `json:"storeIds"`
}

type categoryRequest struct {
	Name         string  `json:"name"`
	Slug         string  `json:"slug"`
	Description  *string `json:"description"`
	ImageURL     *string `json:"imageUrl"`
	ImageAltText *string `json:"imageAltText"`
	ColorCode    *string `json:"colorCode"`
	SortOrder    int     `json:"sortOrder"`
	IsActive     *bool   `json:"isActive"`
	// YYYY-MM-DD, datetime-local, ISO timestamp, or empty/null for "always on" (CAT-09).
	AvailableFrom  *string  `json:"availableFrom"`
	AvailableUntil *string  `json:"availableUntil"`
	BranchIds      []string `json:"storeIds"`
}

// The active flag is passed through as sent, nil included: the store reads
// nil as "leave it alone" on an update and as "on" at create, so a body that
// does not mention visibility cannot change it.
func (r categoryRequest) toWrite() catalogapi.CategoryWrite {
	return catalogapi.CategoryWrite{
		Name: r.Name, Slug: r.Slug, Description: r.Description,
		ImageURL: r.ImageURL, ImageAltText: r.ImageAltText,
		ColorCode: r.ColorCode,
		SortOrder: r.SortOrder, IsActive: r.IsActive,
		AvailableFrom:  parseAdminDatePtr(r.AvailableFrom),
		AvailableUntil: parseAdminDatePtr(r.AvailableUntil),
		BranchIds:      r.BranchIds,
	}
}

// @Summary		List every category, inactive ones included
// @Description	Super admin and finance see the master list. A franchise login sees categories mapped to its branches; a branch login only those mapped to its branch. storeIds is trimmed to the branches the caller can see.
// @Tags			admin-catalogue
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	query	string	false	"Branch to scope to (storeId also accepted); must be one the caller holds"
// @Success		200	{object}	httpx.Envelope
// @Router			/admin/categories [get]
func (h *Handler) ListCategoriesAdmin(w http.ResponseWriter, r *http.Request) {
	rows, err := h.svc.ListCategoriesAdmin(r.Context(), staffClaims(r), requestedBranchID(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out := make([]categoryAdminDTOType, 0, len(rows))
	for i := range rows {
		out = append(out, categoryAdminDTO(&rows[i]))
	}
	httpx.OK(w, out)
}

// @Summary		Get one category
// @Tags			admin-catalogue
// @Produce		json
// @Security		BearerAuth
// @Param			categoryId	path	string	true	"categoryId"
// @Success		200	{object}	httpx.Envelope
// @Router			/admin/categories/{categoryId} [get]
func (h *Handler) GetCategoryAdmin(w http.ResponseWriter, r *http.Request) {
	row, err := h.svc.GetCategoryAdmin(r.Context(), staffClaims(r), chi.URLParam(r, "categoryId"), requestedBranchID(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, categoryAdminDTO(row))
}

// @Summary		Create a category
// @Tags			admin-catalogue
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body	categoryRequest	true	"Request body"
// @Success		201	{object}	httpx.Envelope
// @Router			/admin/categories [post]
func (h *Handler) CreateCategory(w http.ResponseWriter, r *http.Request) {
	var req categoryRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	row, err := h.svc.SaveCategory(r.Context(), staffClaims(r), "", req.toWrite())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Created(w, categoryAdminDTO(row))
}

// @Summary		Update a category
// @Tags			admin-catalogue
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			categoryId	path	string	true	"categoryId"
// @Param			body	body	categoryRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Router			/admin/categories/{categoryId} [patch]
func (h *Handler) UpdateCategory(w http.ResponseWriter, r *http.Request) {
	var req categoryRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	row, err := h.svc.SaveCategory(r.Context(), staffClaims(r), chi.URLParam(r, "categoryId"), req.toWrite())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, categoryAdminDTO(row))
}

type activeRequest struct {
	IsActive bool `json:"isActive"`
}

// @Summary		Turn a category on or off
// @Description	CAT-05 at category scope: instant, no approval step, reflected on the storefront within 30 seconds.
// @Tags			admin-catalogue
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			categoryId	path	string	true	"categoryId"
// @Param			body	body	activeRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Router			/admin/categories/{categoryId}/availability [patch]
func (h *Handler) SetCategoryActive(w http.ResponseWriter, r *http.Request) {
	var req activeRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.svc.SetCategoryActive(r.Context(), staffClaims(r), chi.URLParam(r, "categoryId"), req.IsActive); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]any{"success": true, "isActive": req.IsActive})
}

// @Summary		Delete a category
// @Tags			admin-catalogue
// @Produce		json
// @Security		BearerAuth
// @Param			categoryId	path	string	true	"categoryId"
// @Success		200	{object}	httpx.Envelope
// @Failure		409	{object}	httpx.Envelope	"CATEGORY_NOT_EMPTY"
// @Router			/admin/categories/{categoryId} [delete]
func (h *Handler) DeleteCategory(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteCategory(r.Context(), staffClaims(r), chi.URLParam(r, "categoryId")); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]any{"success": true})
}

// --- Modifier library --------------------------------------------------------

type adminModifierOptionDTO struct {
	ID              string                `json:"id"`
	Name            string                `json:"name"`
	PriceDeltaCents int64                 `json:"priceDeltaCents"`
	MinPriceCents   *int64                `json:"minPriceCents"`
	MaxPriceCents   *int64                `json:"maxPriceCents"`
	IsPreSelected   bool                  `json:"isPreSelected"`
	ImageURL        *string               `json:"imageUrl"`
	IsGlutenFree    bool                  `json:"isGlutenFree"`
	AllergenFlags   []string              `json:"allergenFlags"`
	SortOrder       int                   `json:"sortOrder"`
	IsActive        bool                  `json:"isActive"`
	StorePrices     *[]adminStorePriceDTO `json:"storePrices,omitempty"`
}

type adminModifierGroupDTO struct {
	ID            string                   `json:"id"`
	Name          string                   `json:"name"`
	DisplayName   string                   `json:"displayName"`
	Kind          string                   `json:"kind"`
	Type          string                   `json:"type"`
	IsRequired    bool                     `json:"isRequired"`
	AllowMultiple bool                     `json:"allowMultiple"`
	MinSelect     int                      `json:"minSelect"`
	MaxSelect     *int                     `json:"maxSelect"`
	SortOrder     int                      `json:"sortOrder"`
	Options       []adminModifierOptionDTO `json:"options"`
	ProductIDs    []string                 `json:"productIds"`
}

type adminStorePriceDTO struct {
	BranchId   string `json:"storeId"`
	Store      string `json:"store"`
	PriceCents *int64 `json:"priceCents"`
}

type modifierGroupRequestAdmin struct {
	Name          string `json:"name"`
	DisplayName   string `json:"displayName"`
	Kind          string `json:"kind"`
	Type          string `json:"type"`
	IsRequired    bool   `json:"isRequired"`
	AllowMultiple bool   `json:"allowMultiple"`
	MinSelect     int    `json:"minSelect"`
	// null means no maximum.
	MaxSelect *int                        `json:"maxSelect"`
	SortOrder int                         `json:"sortOrder"`
	Options   []modifierOptionRequestItem `json:"options"`
}

type modifierOptionRequestItem struct {
	Name            string                      `json:"name"`
	PriceDeltaCents int64                       `json:"priceDeltaCents"`
	IsPreSelected   bool                        `json:"isPreSelected"`
	ImageURL        *string                     `json:"imageUrl"`
	IsGlutenFree    bool                        `json:"isGlutenFree"`
	AllergenFlags   []string                    `json:"allergenFlags"`
	StorePrices     []modifierStorePriceRequest `json:"storePrices"`
}

type modifierStorePriceRequest struct {
	BranchId   string `json:"storeId"`
	PriceCents int64  `json:"priceCents"`
}

func (req modifierGroupRequestAdmin) toGroup(id string) catalogapi.ModifierGroup {
	max := 0
	if req.MaxSelect != nil {
		max = *req.MaxSelect
	}
	options := make([]catalogapi.ModifierOption, 0, len(req.Options))
	for _, o := range req.Options {
		var storePrices []catalogapi.BranchModifierRule
		for _, sp := range o.StorePrices {
			price := sp.PriceCents
			storePrices = append(storePrices, catalogapi.BranchModifierRule{
				BranchID:           sp.BranchId,
				PriceOverrideCents: &price,
			})
		}
		options = append(options, catalogapi.ModifierOption{
			Name: o.Name, PriceDeltaCents: o.PriceDeltaCents, IsPreSelected: o.IsPreSelected,
			ImageURL: o.ImageURL, IsGlutenFree: o.IsGlutenFree, AllergenFlags: o.AllergenFlags,
			StorePrices: storePrices,
		})
	}
	return catalogapi.ModifierGroup{
		ID: id, Name: req.Name, DisplayName: req.DisplayName, Kind: req.Kind, Type: req.Type,
		IsRequired: req.IsRequired, AllowMultiple: req.AllowMultiple,
		MinSelect: req.MinSelect, MaxSelect: max, SortOrder: req.SortOrder, Options: options,
	}
}

// @Summary		List the reusable modifier library
// @Description	With branchId (or storeId), only modifier groups used by a product sold at that branch, with option prices as that branch charges them. Branch logins are always scoped to their branch; franchise logins may pick one of theirs.
// @Tags			admin-catalogue
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	query	string	false	"Branch to scope to (storeId also accepted)"
// @Success		200	{object}	httpx.Envelope
// @Router			/admin/modifiers [get]
func (h *Handler) ListModifiers(w http.ResponseWriter, r *http.Request) {
	branchID := requestedBranchID(r)
	groups, err := h.svc.ListModifierGroups(r.Context(), staffClaims(r), branchID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out := make([]adminModifierGroupDTO, 0, len(groups))
	for i := range groups {
		out = append(out, modifierGroupDTO(&groups[i]))
	}
	httpx.OK(w, out)
}

// @Summary		Create a modifier group
// @Tags			admin-catalogue
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body	modifierGroupRequestAdmin	true	"Request body"
// @Success		201	{object}	httpx.Envelope
// @Router			/admin/modifiers [post]
func (h *Handler) CreateModifier(w http.ResponseWriter, r *http.Request) {
	var req modifierGroupRequestAdmin
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	id, err := h.svc.SaveModifierGroup(r.Context(), staffClaims(r), req.toGroup(""))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Created(w, map[string]any{"id": id})
}

// @Summary		Update a modifier group
// @Tags			admin-catalogue
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			modifierId	path	string	true	"modifierId"
// @Param			body	body	modifierGroupRequestAdmin	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Router			/admin/modifiers/{modifierId} [patch]
func (h *Handler) UpdateModifier(w http.ResponseWriter, r *http.Request) {
	var req modifierGroupRequestAdmin
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	id, err := h.svc.SaveModifierGroup(r.Context(), staffClaims(r), req.toGroup(chi.URLParam(r, "modifierId")))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]any{"id": id})
}

// @Summary		Delete a modifier group
// @Tags			admin-catalogue
// @Produce		json
// @Security		BearerAuth
// @Param			modifierId	path	string	true	"modifierId"
// @Success		200	{object}	httpx.Envelope
// @Router			/admin/modifiers/{modifierId} [delete]
func (h *Handler) DeleteModifier(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteModifierGroup(r.Context(), staffClaims(r), chi.URLParam(r, "modifierId")); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]any{"success": true})
}

type applyModifierRequest struct {
	ProductIDs []string `json:"productIds"`
}

// @Summary		Apply a modifier group to a set of products
// @Description	Replaces the whole set. The dialog sends the complete list it wants, so a diff would leave a window where a product belongs to neither the old set nor the new one.
// @Tags			admin-catalogue
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			modifierId	path	string	true	"modifierId"
// @Param			body	body	applyModifierRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Router			/admin/modifiers/{modifierId}/products [put]
func (h *Handler) ApplyModifierToProducts(w http.ResponseWriter, r *http.Request) {
	var req applyModifierRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.svc.ApplyModifierToProducts(r.Context(), staffClaims(r), chi.URLParam(r, "modifierId"), req.ProductIDs); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]any{"success": true, "count": len(req.ProductIDs)})
}

// --- Coupons -----------------------------------------------------------------

type couponDTOType struct {
	ID                string   `json:"id"`
	Code              string   `json:"code"`
	Name              string   `json:"name"`
	Type              string   `json:"type"`
	DiscountValue     int64    `json:"discountValue"`
	Status            string   `json:"status"`
	AppliesTo         string   `json:"appliesTo"`
	TargetIDs         []string `json:"targetIds"`
	UsageLimit        *int     `json:"usageLimit"`
	RedeemedCount     int      `json:"redeemedCount"`
	HasActiveDates    bool     `json:"hasActiveDates"`
	StartDate         *string  `json:"startDate"`
	EndDate           *string  `json:"endDate"`
	HasMinimumOrder   bool     `json:"hasMinimumOrder"`
	MinimumOrderCents int64    `json:"minimumOrderCents"`
	BranchID          *string  `json:"branchId"`
	ShowOnCheckout    bool     `json:"showOnCheckout"`
	CreatedAt         string   `json:"createdAt"`
}

type couponRequest struct {
	Code              string   `json:"code"`
	Name              string   `json:"name"`
	Type              string   `json:"type"`
	DiscountValue     int64    `json:"discountValue"`
	Status            string   `json:"status"`
	AppliesTo         string   `json:"appliesTo"`
	TargetIDs         []string `json:"targetIds"`
	UsageLimit        *int     `json:"usageLimit"`
	HasActiveDates    bool     `json:"hasActiveDates"`
	StartDate         string   `json:"startDate"`
	EndDate           string   `json:"endDate"`
	HasMinimumOrder   bool     `json:"hasMinimumOrder"`
	MinimumOrderCents int64    `json:"minimumOrderCents"`
	BranchID          *string  `json:"branchId"`
	ShowOnCheckout    bool     `json:"showOnCheckout"`
}

func (req couponRequest) toCoupon() catalogapi.Coupon {
	return catalogapi.Coupon{
		Code: req.Code, Name: req.Name, Type: req.Type, DiscountValue: req.DiscountValue,
		Status: req.Status, AppliesTo: req.AppliesTo, TargetIDs: req.TargetIDs,
		UsageLimit: req.UsageLimit, HasActiveDates: req.HasActiveDates,
		StartsAt: parseAdminDate(req.StartDate), EndsAt: parseAdminDate(req.EndDate),
		HasMinimumOrder: req.HasMinimumOrder, MinimumOrderCents: req.MinimumOrderCents,
		BranchID: req.BranchID, ShowOnCheckout: req.ShowOnCheckout,
	}
}

// @Summary		List coupons
// @Description	A branch-scoped user sees network-wide coupons (live at their outlet) plus their own — never another branch's local promotion. With branchId (or storeId), the list is what is live at that branch: network-wide coupons plus that branch's own. The branch must be one the caller holds.
// @Tags			admin-catalogue
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	query	string	false	"Branch to scope to (storeId also accepted)"
// @Success		200	{object}	httpx.Envelope
// @Router			/admin/coupons [get]
func (h *Handler) ListCoupons(w http.ResponseWriter, r *http.Request) {
	coupons, err := h.svc.ListCoupons(r.Context(), staffClaims(r), requestedBranchID(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out := make([]couponDTOType, 0, len(coupons))
	for i := range coupons {
		out = append(out, couponDTO(&coupons[i]))
	}
	httpx.OK(w, out)
}

// @Summary		Create a coupon
// @Description	type is the discount rule: percentage, fixed, free_delivery or free_shipping. appliesTo is who or what it is for: all, products, categories (with targetIds) or new_customer. A new_customer coupon is for a signed-in customer's first order, covers the whole basket, never has a date window (startDate/endDate are stored as null), and only one may be active at a time (409 NEW_CUSTOMER_COUPON_EXISTS).
// @Tags			admin-catalogue
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body	couponRequest	true	"Request body"
// @Success		201	{object}	httpx.Envelope
// @Failure		409	{object}	httpx.Envelope	"COUPON_CODE_IN_USE or NEW_CUSTOMER_COUPON_EXISTS"
// @Router			/admin/coupons [post]
func (h *Handler) CreateCoupon(w http.ResponseWriter, r *http.Request) {
	var req couponRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	c, err := h.svc.SaveCoupon(r.Context(), staffClaims(r), "", req.toCoupon())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Created(w, couponDTO(c))
}

// @Summary		Update a coupon
// @Tags			admin-catalogue
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			couponId	path	string	true	"couponId"
// @Param			body	body	couponRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		409	{object}	httpx.Envelope	"COUPON_CODE_IN_USE or NEW_CUSTOMER_COUPON_EXISTS"
// @Router			/admin/coupons/{couponId} [patch]
func (h *Handler) UpdateCoupon(w http.ResponseWriter, r *http.Request) {
	var req couponRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	c, err := h.svc.SaveCoupon(r.Context(), staffClaims(r), chi.URLParam(r, "couponId"), req.toCoupon())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, couponDTO(c))
}

type couponStatusRequest struct {
	Status string `json:"status"`
}

// @Summary		Change a coupon's status
// @Tags			admin-catalogue
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			couponId	path	string	true	"couponId"
// @Param			body	body	couponStatusRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		409	{object}	httpx.Envelope	"NEW_CUSTOMER_COUPON_EXISTS — activating a second new-customer coupon"
// @Router			/admin/coupons/{couponId}/status [post]
func (h *Handler) SetCouponStatus(w http.ResponseWriter, r *http.Request) {
	var req couponStatusRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.svc.SetCouponStatus(r.Context(), staffClaims(r), chi.URLParam(r, "couponId"), req.Status); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]any{"success": true, "status": req.Status})
}

// @Summary		Delete a coupon
// @Description	Soft delete. Redemptions reference the coupon and orders that used it keep their discount, so the row stays.
// @Tags			admin-catalogue
// @Produce		json
// @Security		BearerAuth
// @Param			couponId	path	string	true	"couponId"
// @Success		200	{object}	httpx.Envelope
// @Router			/admin/coupons/{couponId} [delete]
func (h *Handler) DeleteCoupon(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteCoupon(r.Context(), staffClaims(r), chi.URLParam(r, "couponId")); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]any{"success": true})
}

type productModifiersRequest struct {
	ModifierGroupIDs []string `json:"modifierGroupIds"`
}

// @Summary		Set which library modifier groups a product carries
// @Description	The product-form side of the relationship: "this product, these modifiers". Replaces the whole set. The mirror of PUT /admin/modifiers/{id}/products, which sets the other side.
// @Tags			admin-catalogue
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			id	path	string	true	"id"
// @Param			body	body	productModifiersRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/products/{id}/modifiers [put]
func (h *Handler) SetProductModifiers(w http.ResponseWriter, r *http.Request) {
	var req productModifiersRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.svc.SetProductModifierGroups(r.Context(), staffClaims(r), chi.URLParam(r, "id"), req.ModifierGroupIDs); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]any{"success": true, "count": len(req.ModifierGroupIDs)})
}
