package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/httpx"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/catalog/internal/domain"
)

// --- Admin: master catalogue -----------------------------------------------------

// @Summary		Create a master-catalogue product
// @Tags			admin-catalogue
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body	productRequest	true	"Request body"
// @Success		201	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/admin/products [post]
func (h *Handler) CreateProduct(w http.ResponseWriter, r *http.Request) {
	var req productRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p, err := h.svc.CreateProduct(r.Context(), staffClaims(r), productFromRequest(req))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Created(w, productDetailDTO(&domain.ProductWithAvailability{Product: *p, Availability: catalogapi.Availability{Available: p.IsActive}}, nil))
}

// @Summary		List master-catalogue products
// @Description	Super admin and finance see the master catalogue with every store price. A franchise login sees products mapped to its branches; a branch login only products mapped to its branch (and enabled in a category mapped there), priced with that branch's overrides.
// @Tags			admin-catalogue
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	query	string	false	"Branch to scope to (storeId also accepted); must be one the caller holds"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/admin/products [get]
func (h *Handler) ListProductsAdmin(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page := atoiOr(q.Get("page"), 1)
	if page < 1 {
		page = 1
	}
	// 200 rather than unlimited: a console that asks for everything is a
	// console that will one day ask for fifty thousand rows.
	perPage := atoiOr(q.Get("perPage"), 25)
	if perPage < 1 || perPage > 200 {
		perPage = 25
	}

	var showOnPos, showOnWebsite *bool
	if v := q.Get("showOnPos"); v != "" {
		b := v == "true" || v == "1"
		showOnPos = &b
	}
	if v := q.Get("showOnWebsite"); v != "" {
		b := v == "true" || v == "1"
		showOnWebsite = &b
	}

	branchID := requestedBranchID(r)

	list, total, err := h.svc.ListProductsAdminPage(r.Context(), staffClaims(r),
		q.Get("categoryId"), strings.TrimSpace(q.Get("search")), showOnPos, showOnWebsite, perPage, (page-1)*perPage, branchID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out := make([]productListDTOType, 0, len(list))
	for _, p := range list {
		pp := p
		out = append(out, productSummaryDTO(&pp))
	}
	// total is the number of matches, not the size of this page — the pager
	// needs the former to know how many pages there are.
	httpx.OKWithMeta(w, out, map[string]int{"total": total, "page": page, "perPage": perPage})
}

// atoiOr keeps query-string parsing to one line at the call sites: a missing
// or unparseable value falls back rather than failing the request.
func atoiOr(value string, fallback int) int {
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return n
}

// @Summary		Get one master-catalogue product
// @Tags			admin-catalogue
// @Summary		Get a master-catalogue product
// @Tags			admin-catalogue
// @Produce		json
// @Security		BearerAuth
// @Param			id	path	string	true	"id"
// @Param			storeId	query	string	false	"Branch ID (store ID) override for branch-scoped views"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/products/{id} [get]
func (h *Handler) GetProductAdmin(w http.ResponseWriter, r *http.Request) {
	branchID := r.URL.Query().Get("storeId")
	if branchID == "" {
		branchID = r.URL.Query().Get("branchId")
	}
	if branchID == "" {
		branchID = r.Header.Get("X-Branch-ID")
	}
	if branchID == "" {
		branchID = r.Header.Get("X-Store-ID")
	}

	claims := staffClaims(r)
	p, err := h.svc.GetProductAdmin(r.Context(), claims, chi.URLParam(r, "id"), branchID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, adminProductDTO(p))
}

// @Summary		Update a master-catalogue product
// @Tags			admin-catalogue
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			id	path	string	true	"id"
// @Param			body	body	productRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/products/{id} [patch]
func (h *Handler) UpdateProduct(w http.ResponseWriter, r *http.Request) {
	var req productPatchRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	patch, err := productPatchFromRequest(req)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p, err := h.svc.UpdateProduct(r.Context(), staffClaims(r), chi.URLParam(r, "id"), patch)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, productDetailDTO(&domain.ProductWithAvailability{Product: *p, Availability: catalogapi.Availability{Available: p.IsActive}}, nil))
}

// @Summary		Discontinue a master-catalogue product
// @Tags			admin-catalogue
// @Produce		json
// @Security		BearerAuth
// @Param			id	path	string	true	"id"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/products/{id} [delete]
func (h *Handler) DeleteProduct(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteProduct(r.Context(), staffClaims(r), chi.URLParam(r, "id")); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"success": true})
}
