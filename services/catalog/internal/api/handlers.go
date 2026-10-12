package api

import (
	"net/http"
	"net/url"
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
