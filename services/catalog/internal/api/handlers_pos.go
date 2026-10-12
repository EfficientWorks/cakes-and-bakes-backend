package api

import (
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/httpx"
)

/*
 * The till's catalogue reads.
 *
 * These delegate to the storefront handlers above rather than reimplementing
 * them: a counter sells the same products under the same availability and
 * scheduling rules, and two code paths that could disagree about what is on
 * sale is a bug waiting to be found at a counter.
 *
 * They exist as named functions only so the generated API documentation shows
 * them. Swagger takes one path per function, so these routes — mounted under
 * /pos by reusing a storefront handler — were live but absent from /docs, and
 * a POS client had no way to discover them short of reading the router.
 */

// @Summary		List categories for the till
// @Description	Only the categories mapped to the signed-in store (or the branchId given), with at least one product mapped to it.
// @Tags			pos
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	query		string	false	"Branch to scope to; defaults to the staff member's own branch"
// @Success		200			{object}	httpx.Envelope
// @Router			/pos/categories [get]
func (h *Handler) ListCategoriesPOS(w http.ResponseWriter, r *http.Request) {
	branchID := h.resolveBranchForStaffRequest(r)
	cats, err := h.svc.ListCategoriesForBranch(r.Context(), branchID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	// The till lists categories alphabetically (A→Z, ignoring case), so
	// staff find one by name; the storefront keeps the admin sort order.
	sort.SliceStable(cats, func(i, j int) bool {
		return strings.ToLower(strings.TrimSpace(cats[i].Name)) < strings.ToLower(strings.TrimSpace(cats[j].Name))
	})
	out := make([]categoryDTOType, 0, len(cats))
	for _, c := range cats {
		out = append(out, categoryDTO(&c))
	}
	httpx.OK(w, out)
}

// @Summary		Get one category for the till
// @Tags			pos
// @Produce		json
// @Security		BearerAuth
// @Param			categoryId	path		string	true	"Category ID"
// @Success		200			{object}	httpx.Envelope
// @Failure		404			{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/pos/categories/{categoryId} [get]
func (h *Handler) GetCategoryPOS(w http.ResponseWriter, r *http.Request) {
	h.GetCategoryStorefront(w, r)
}

func (h *Handler) resolveBranchForStaffRequest(r *http.Request) string {
	q := r.URL.Query()
	branchID := q.Get("branchId")
	if branchID == "" {
		branchID = q.Get("storeId")
	}
	if branchID == "" {
		branchID = chi.URLParam(r, "branchId")
	}
	if branchID == "" {
		branchID = chi.URLParam(r, "storeId")
	}
	if branchID == "" {
		branchID = r.Header.Get("X-Branch-ID")
	}
	if branchID == "" {
		branchID = r.Header.Get("X-Store-ID")
	}
	claims := staffClaims(r)
	if claims != nil {
		if resolved := h.svc.ResolveBranchIDForStaff(r.Context(), claims, branchID); resolved != "" {
			return resolved
		}
	}
	return branchID
}

// @Summary		List products for the till
// @Description	Only the products mapped to the signed-in store (or the branchId given), priced with that store's override where one is set.
// @Tags			pos
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	query		string	false	"Branch to scope availability to"
// @Param			categoryId	query		string	false	"Filter by category"
// @Param			search		query		string	false	"Free-text search"
// @Param			showOnPos	query		bool	false	"Filter by show on POS"
// @Param			sort			query	string	false	"most_gifted (default), price_asc, price_desc, newest, name_asc"
// @Param			page			query	int		false	"1-based page number"
// @Param			limit		query		int		false	"Page size"
// @Param			offset		query		int		false	"Rows to skip"
// @Success		200			{object}	httpx.Envelope
// @Router			/pos/products [get]
func (h *Handler) ListProductsPOS(w http.ResponseWriter, r *http.Request) {
	branchID := h.resolveBranchForStaffRequest(r)

	q := r.URL.Query()
	if q.Get("showOnPos") == "" {
		q.Set("showOnPos", "true")
	}
	// The till searches names only — see ProductFilter.SearchNameOnly.
	q.Set("searchScope", "name")
	if branchID != "" {
		q.Set("branchId", branchID)
	}
	r.URL.RawQuery = q.Encode()

	filter, err := parseProductFilter(q)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	// A till only sells what its store is mapped to, at that store's price.
	// The storefront lists everything and marks the rest unavailable; here
	// the unmapped products are left out of the page and the count entirely.
	filter.MappedBranchID = branchID

	page, err := h.svc.SearchProductsForStorefront(r.Context(), branchID, filter, parseDate(q.Get("date")))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	out := make([]posProductListDTOType, 0, len(page.Products))
	breakdown := map[string]int{}
	for _, p := range page.Products {
		dto := posProductListDTO(&p)
		dto.StorePrices = nil
		dto.BranchIds = nil
		out = append(out, dto)
		breakdown[p.CategorySlug]++
	}

	totalPages := 0
	if page.Limit > 0 {
		totalPages = (page.Total + page.Limit - 1) / page.Limit
	}
	httpx.OKWithMeta(w, out, productListMeta{
		Page:              page.Page,
		PerPage:           page.Limit,
		Total:             page.Total,
		TotalPages:        totalPages,
		CategoryBreakdown: breakdown,
	})
}

// @Summary		Get one product for the till by id or slug
// @Description	Full product with its modifier groups — what the counter needs to build a line.
// @Tags			pos
// @Produce		json
// @Security		BearerAuth
// @Param			slug		path		string	true	"Product slug or ID"
// @Param			branchId	query		string	false	"Branch to scope availability to"
// @Success		200			{object}	httpx.Envelope
// @Failure		404			{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/pos/products/{slug} [get]
func (h *Handler) GetProductPOS(w http.ResponseWriter, r *http.Request) {
	branchID := h.resolveBranchForStaffRequest(r)
	q := r.URL.Query()
	date := parseDate(q.Get("date"))

	slugOrID := chi.URLParam(r, "slug")
	if slugOrID == "" {
		slugOrID = chi.URLParam(r, "id")
	}

	product, similar, err := h.svc.GetProductForStorefront(r.Context(), slugOrID, branchID, date)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, posProductDetailDTO(product, similar))
}
