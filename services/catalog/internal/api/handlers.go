package api

import (
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/httpx"
	"bakehouse/pkg/media"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/catalog/internal/domain"
)

type Handler struct {
	svc *domain.Service
	// media issues image upload tickets. Held by the catalogue because
	// everything that carries an image — products, categories, modifier
	// options — is catalogue-owned.
	media media.Storer
	// videos stores product videos (one per product). Nil disables video
	// upload and delivery.
	videos media.VideoStorer
}

// UseVideos sets the product video store.
func (h *Handler) UseVideos(v media.VideoStorer) { h.videos = v }

func NewHandler(svc *domain.Service, storer media.Storer) *Handler {
	return &Handler{svc: svc, media: storer}
}

func staffClaims(r *http.Request) *auth.StaffClaims {
	c, _ := auth.StaffFromContext(r.Context())
	return c
}

func parseDate(s string) time.Time {
	if s == "" {
		return time.Now()
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t
	}
	return time.Now()
}

// --- Storefront ----------------------------------------------------------------

// @Summary		List product categories
// @Tags			storefront
// @Produce		json
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/storefront/categories [get]
func (h *Handler) ListCategories(w http.ResponseWriter, r *http.Request) {
	cats, err := h.svc.ListCategories(r.Context())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out := make([]categoryDTOType, 0, len(cats))
	for _, c := range cats {
		out = append(out, categoryDTO(&c))
	}
	httpx.OK(w, out)
}

// @Summary		Get one category
// @Tags			storefront
// @Produce		json
// @Param			categoryId	path		string	true	"Category ID"
// @Success		200			{object}	httpx.Envelope
// @Failure		404			{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/storefront/categories/{categoryId} [get]
func (h *Handler) GetCategoryStorefront(w http.ResponseWriter, r *http.Request) {
	cat, err := h.svc.GetCategory(r.Context(), chi.URLParam(r, "categoryId"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, categoryDTO(cat))
}

// ListProducts drives the Shop Now grid: search, category tabs, occasion and
// dietary chips, price slider, sort and paging.
//
// Multi-value filters accept either repeated parameters or one comma-separated
// value, so `?occasion=birthday&occasion=anniversary` and
// `?occasion=birthday,anniversary` behave identically — front ends serialise
// checkbox groups both ways.
//
//	@Summary		List products available at a branch
//	@Description	Storefront product grid with search, faceted filters, price range, sort and pagination. Sold-out items are still returned, flagged unavailable with a reason and next available date, rather than disappearing from the grid.
//	@Tags			storefront
//	@Produce		json
//	@Param			branchId		query		string	false	"Branch to resolve availability against. Omit to browse the whole catalogue."
//	@Param			date			query		string	false	"Availability date (YYYY-MM-DD), defaults to today"
//	@Param			search			query		string	false	"Partial match on product name and description"
//	@Param			categoryId		query		string	false	"Category tab"
//	@Param			occasion		query		string	false	"Occasion tags; repeated or comma-separated. Matches ANY"
//	@Param			tag				query		string	false	"Seasonal tag ids (from /storefront/seasonal-tags); repeated or comma-separated. Matches ANY"
//	@Param			dietary			query		string	false	"Dietary flags; repeated or comma-separated. Matches ALL"
//	@Param			minPriceCents	query		int		false	"Lowest starting price, in cents"
//	@Param			maxPriceCents	query		int		false	"Highest starting price, in cents"
//	@Param			bestSeller		query		bool	false	"Restrict to best sellers"
//	@Param			sort			query		string	false	"most_gifted (default), price_asc, price_desc, newest, name_asc"
//	@Param			page			query		int		false	"1-based page number"
//	@Param			limit			query		int		false	"Page size (default 50, max 200)"
//	@Success		200				{object}	httpx.Envelope{data=[]productListDTOType,meta=productListMeta}
//	@Failure		400				{object}	httpx.Envelope	"VALIDATION_FAILED"
//	@Failure		404				{object}	httpx.Envelope	"NOT_FOUND — unknown branch"
//	@Router			/storefront/products [get]
func (h *Handler) ListProducts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	branchID := q.Get("branchId")
	if branchID == "" {
		branchID = q.Get("storeId")
	}
	if branchID == "" {
		branchID = r.Header.Get("X-Branch-ID")
	}
	if branchID == "" {
		branchID = r.Header.Get("X-Store-ID")
	}

	filter, err := parseProductFilter(q)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	// For storefront listing, default to showOnWebsite=true when neither flag is explicitly set.
	if filter.ShowOnWebsite == nil && filter.ShowOnPos == nil {
		tru := true
		filter.ShowOnWebsite = &tru
	}

	page, err := h.svc.SearchProductsForStorefront(r.Context(), branchID, filter, parseDate(q.Get("date")))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	out := make([]productListDTOType, 0, len(page.Products))
	breakdown := map[string]int{}
	for _, p := range page.Products {
		dto := productListDTO(&p)
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

// productListMeta is the pager and tab-count block on the Shop Now grid.
type productListMeta struct {
	Page       int `json:"page" example:"1"`
	PerPage    int `json:"per_page" example:"50"`
	Total      int `json:"total" example:"9"`
	TotalPages int `json:"total_pages" example:"1"`
	// CategoryBreakdown counts the current page by category slug, for the tab
	// badges above the grid.
	CategoryBreakdown map[string]int `json:"categoryBreakdown"`
}

// parseProductFilter reads the grid's query string, rejecting values the grid
// could not have produced rather than silently ignoring them.
func parseProductFilter(q url.Values) (catalogapi.ProductFilter, error) {
	f := catalogapi.ProductFilter{
		Search:     strings.TrimSpace(q.Get("search")),
		CategoryID: q.Get("categoryId"),
		// Also accepted by slug. A till tabs across categories by the name it
		// was given in the list, and making it carry a uuid around to ask for
		// "Bouquets" is friction with nothing behind it.
		CategorySlug:   strings.TrimSpace(q.Get("categorySlug")),
		Occasions:      multiValue(q, "occasion"),
		TagIDs:         multiValue(q, "tag"),
		Dietary:        multiValue(q, "dietary"),
		BestSellerOnly: q.Get("bestSeller") == "true",
		Sort:           q.Get("sort"),
	}
	f.SearchNameOnly = q.Get("searchScope") == "name"

	switch f.Sort {
	case "", catalogapi.SortMostGifted, catalogapi.SortPriceAsc, catalogapi.SortPriceDesc,
		catalogapi.SortNewest, catalogapi.SortNameAsc:
	default:
		return f, apperr.Validation("sort must be one of most_gifted, price_asc, price_desc, newest, name_asc.")
	}

	var err error
	if f.MinPriceCents, err = optionalCents(q.Get("minPriceCents"), "minPriceCents"); err != nil {
		return f, err
	}
	if f.MaxPriceCents, err = optionalCents(q.Get("maxPriceCents"), "maxPriceCents"); err != nil {
		return f, err
	}
	if f.MinPriceCents != nil && f.MaxPriceCents != nil && *f.MinPriceCents > *f.MaxPriceCents {
		return f, apperr.Validation("minPriceCents must not be greater than maxPriceCents.")
	}

	if v := q.Get("showOnPos"); v != "" {
		b := v == "true" || v == "1"
		f.ShowOnPos = &b
	}
	if v := q.Get("showOnWebsite"); v != "" {
		b := v == "true" || v == "1"
		f.ShowOnWebsite = &b
	}

	if f.Page, err = optionalInt(q.Get("page"), "page"); err != nil {
		return f, err
	}
	if f.Limit, err = optionalInt(q.Get("limit"), "limit"); err != nil {
		return f, err
	}
	return f, nil
}
func parsePaging(q url.Values) (page, limit int, err error) {
	if limit, err = optionalInt(q.Get("limit"), "limit"); err != nil {
		return 0, 0, err
	}
	if limit == 0 {
		if limit, err = optionalInt(q.Get("per_page"), "per_page"); err != nil {
			return 0, 0, err
		}
	}
	if page, err = optionalInt(q.Get("page"), "page"); err != nil {
		return 0, 0, err
	}

	raw := q.Get("offset")
	if raw == "" || page != 0 {
		return page, limit, nil // page wins when both are sent
	}
	offset, convErr := strconv.Atoi(raw)
	if convErr != nil || offset < 0 {
		return 0, 0, apperr.Validation("offset must be a whole number, zero or greater.")
	}
	size := limit
	if size == 0 {
		size = catalogapi.DefaultProductPageSize
	}
	if offset%size != 0 {
		return 0, 0, apperr.Validation("offset must be a multiple of limit.")
	}
	return offset/size + 1, limit, nil
}

// multiValue reads a filter that may arrive repeated or comma-separated.
func multiValue(q url.Values, key string) []string {
	var out []string
	for _, raw := range q[key] {
		for _, part := range strings.Split(raw, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func optionalCents(raw, field string) (*int64, error) {
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v < 0 {
		return nil, apperr.Validation(field + " must be a whole number of cents, zero or greater.")
	}
	return &v, nil
}

func optionalInt(raw, field string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 1 {
		return 0, apperr.Validation(field + " must be a positive whole number.")
	}
	return v, nil
}

// StorefrontFacets returns the filter panel's own options.
//
//	@Summary		Filter options for the product grid
//	@Description	Occasion and dietary chips that actually match at least one active product, plus the price range the slider should span. Prevents the panel offering a filter that returns nothing.
//	@Tags			storefront
//	@Produce		json
//	@Success		200	{object}	httpx.Envelope{data=facetsDTOType}
//	@Router			/storefront/product-filters [get]
func (h *Handler) StorefrontFacets(w http.ResponseWriter, r *http.Request) {
	facets, err := h.svc.Facets(r.Context())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, facetsDTOType{
		Occasions:     facets.Occasions,
		Tags:          seasonalTagDTOs(facets.Tags),
		Dietary:       facets.Dietary,
		MinPriceCents: facets.MinPriceCents,
		MaxPriceCents: facets.MaxPriceCents,
		Sorts: []sortOptionDTO{
			{Value: catalogapi.SortMostGifted, Label: "Most Gifted"},
			{Value: catalogapi.SortPriceAsc, Label: "Price: Low to High"},
			{Value: catalogapi.SortPriceDesc, Label: "Price: High to Low"},
			{Value: catalogapi.SortNewest, Label: "Newest"},
			{Value: catalogapi.SortNameAsc, Label: "Name: A to Z"},
		},
	})
}

type facetsDTOType struct {
	Occasions []string `json:"occasions"`
	// Seasonal tag chips (active tags with products); filter with ?tag=<id>.
	Tags          []seasonalTagDTOType `json:"tags"`
	Dietary       []string             `json:"dietary"`
	MinPriceCents int64                `json:"minPriceCents" example:"3200"`
	MaxPriceCents int64                `json:"maxPriceCents" example:"14200"`
	Sorts         []sortOptionDTO      `json:"sorts"`
}

type sortOptionDTO struct {
	Value string `json:"value" example:"most_gifted"`
	Label string `json:"label" example:"Most Gifted"`
}

// @Summary		Get one product with its options and availability
// @Tags			storefront
// @Produce		json
// @Param			slug	path	string	true	"slug"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/storefront/products/{slug} [get]
func (h *Handler) GetProduct(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	branchID := q.Get("branchId")
	if branchID == "" {
		branchID = q.Get("storeId")
	}
	if branchID == "" {
		branchID = r.Header.Get("X-Branch-ID")
	}
	if branchID == "" {
		branchID = r.Header.Get("X-Store-ID")
	}
	date := parseDate(q.Get("date"))

	product, similar, err := h.svc.GetProductForStorefront(r.Context(), chi.URLParam(r, "slug"), branchID, date)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, productDetailDTO(product, similar))
}

// @Summary		Get one product with its options and availability by branch
// @Tags			storefront
// @Produce		json
// @Param			slug	path	string	true	"slug"
// @Param			branchId	path	string	true	"branchId"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/storefront/products/{slug}/{branchId} [get]
func (h *Handler) GetProductByBranchId(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	date := parseDate(q.Get("date"))

	branchID := chi.URLParam(r, "branchId")
	if branchID == "" {
		branchID = chi.URLParam(r, "storeId")
	}

	product, similar, err := h.svc.GetProductForStorefront(r.Context(), chi.URLParam(r, "slug"), branchID, date)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, productDetailDTO(product, similar))
}

// @Summary		Get site content blocks by key
// @Tags			storefront
// @Produce		json
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/storefront/content [get]
func (h *Handler) GetContent(w http.ResponseWriter, r *http.Request) {
	var keys []string
	if raw := r.URL.Query().Get("keys"); raw != "" {
		keys = splitCSV(raw)
	}
	content, err := h.svc.GetContent(r.Context(), keys)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, content)
}

func splitCSV(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	return out
}

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

// --- Admin: branch availability --------------------------------------------------

// @Summary		Get the branch availability grid
// @Tags			admin-availability
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	path	string	true	"branchId"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/branches/{branchId}/availability [get]
func (h *Handler) GetAvailabilityGrid(w http.ResponseWriter, r *http.Request) {
	date := parseDate(r.URL.Query().Get("date"))
	rows, err := h.svc.GetAvailabilityGrid(r.Context(), staffClaims(r), chi.URLParam(r, "branchId"), date)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out := make([]gridRowDTOType, 0, len(rows))
	for _, row := range rows {
		out = append(out, gridRowDTO(&row))
	}
	httpx.OK(w, out)
}

type availabilityRequest struct {
	BranchEnabled      *bool    `json:"branchEnabled"`
	Reason             string   `json:"reason"`
	AvailableDays      []string `json:"availableDays"`
	PriceOverrideCents *int64   `json:"priceOverrideCents"`
	AvailableFrom      string   `json:"availableFrom"`
	AvailableUntil     string   `json:"availableUntil"`
}

// @Summary		Set a product's weekly availability at a branch
// @Tags			admin-availability
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	path	string	true	"branchId"
// @Param			productId	path	string	true	"productId"
// @Param			body	body	availabilityRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/branches/{branchId}/products/{productId}/availability [patch]
func (h *Handler) UpdateAvailability(w http.ResponseWriter, r *http.Request) {
	var req availabilityRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	patch := domain.AvailabilityPatch{BranchEnabled: req.BranchEnabled, Reason: req.Reason, AvailableDays: req.AvailableDays, PriceOverrideCents: req.PriceOverrideCents}
	if req.AvailableFrom != "" {
		if t := parseAdminDate(req.AvailableFrom); t != nil {
			patch.AvailableFrom = t
		}
	}
	if req.AvailableUntil != "" {
		if t := parseAdminDate(req.AvailableUntil); t != nil {
			patch.AvailableUntil = t
		}
	}

	out, err := h.svc.UpdateAvailability(r.Context(), staffClaims(r), chi.URLParam(r, "branchId"), chi.URLParam(r, "productId"), patch)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, ruleDTO(out))
}

type dateOverrideRequest struct {
	Date      string `json:"date"`
	Available bool   `json:"available"`
	Reason    string `json:"reason"`
}

// @Summary		Override a product's availability for one date
// @Tags			admin-availability
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	path	string	true	"branchId"
// @Param			productId	path	string	true	"productId"
// @Param			body	body	dateOverrideRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/branches/{branchId}/products/{productId}/date-override [post]
func (h *Handler) CreateDateOverride(w http.ResponseWriter, r *http.Request) {
	var req dateOverrideRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if req.Date == "" {
		httpx.WriteError(w, r, apperr.Validation("date is required (YYYY-MM-DD)."))
		return
	}
	out, err := h.svc.AddDateOverride(r.Context(), staffClaims(r), chi.URLParam(r, "branchId"), chi.URLParam(r, "productId"),
		catalogapi.DateOverride{Date: req.Date, Available: req.Available, Reason: req.Reason})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, ruleDTO(out))
}

type bulkDisableRequest struct {
	ProductIDs []string `json:"productIds"`
	Reason     string   `json:"reason"`
}

// @Summary		Disable several products at a branch at once
// @Tags			admin-availability
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	path	string	true	"branchId"
// @Param			body	body	bulkDisableRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/branches/{branchId}/availability/bulk-disable [post]
func (h *Handler) BulkDisable(w http.ResponseWriter, r *http.Request) {
	var req bulkDisableRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	count, err := h.svc.BulkDisable(r.Context(), staffClaims(r), chi.URLParam(r, "branchId"), req.ProductIDs, req.Reason)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]int{"disabled": count})
}

type reasonOnlyRequest struct {
	Reason string `json:"reason"`
}

// @Summary		Mark a product sold out for today
// @Tags			admin-availability
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	path	string	true	"branchId"
// @Param			productId	path	string	true	"productId"
// @Param			body	body	reasonOnlyRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/branches/{branchId}/products/{productId}/sold-out [post]
func (h *Handler) SetSoldOut(w http.ResponseWriter, r *http.Request) {
	var req reasonOnlyRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.svc.SetSoldOut(r.Context(), staffClaims(r), chi.URLParam(r, "branchId"), chi.URLParam(r, "productId"), req.Reason); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"success": true})
}

// @Summary		Restore a product marked sold out today
// @Tags			admin-availability
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	path	string	true	"branchId"
// @Param			productId	path	string	true	"productId"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/branches/{branchId}/products/{productId}/restore [post]
func (h *Handler) RestoreToday(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.RestoreToday(r.Context(), staffClaims(r), chi.URLParam(r, "branchId"), chi.URLParam(r, "productId")); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"success": true})
}

// --- Admin: content ----------------------------------------------------------------

type contentRequest struct {
	Value string `json:"value"`
	Type  string `json:"type"`
}

// @Summary		Set a site content block
// @Tags			admin-catalogue
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			key	path	string	true	"key"
// @Param			body	body	contentRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/content/{key} [patch]
func (h *Handler) SetContent(w http.ResponseWriter, r *http.Request) {
	var req contentRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.svc.SetContent(r.Context(), staffClaims(r), chi.URLParam(r, "key"), req.Value, req.Type); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"success": true})
}

// @Summary      Get site content block by key
// @Tags         admin-site-content
// @Produce      json
// @Security     BearerAuth
// @Param        key path string true "Content key"
// @Success      200 {object} httpx.Envelope
// @Failure      404 {object} httpx.Envelope "NOT_FOUND"
// @Failure      500 {object} httpx.Envelope "INTERNAL_ERROR"
// @Router       /admin/content/{key} [get]
func (h *Handler) GetContentByKey(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")

	if strings.TrimSpace(key) == "" {
		httpx.WriteError(w, r, apperr.Validation("key is required"))
		return
	}

	content, err := h.svc.GetContentByKey(r.Context(), key)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	httpx.OK(w, content)
}

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
