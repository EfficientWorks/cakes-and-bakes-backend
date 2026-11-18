package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/httpx"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/catalog/internal/domain"
)

/*
 * Flavours: the home page's "Discover our flavours" section.
 *
 * Storefront: GET /storefront/flavours lists the cards (active flavours with
 * at least one active product, in sortOrder). Each card's "Shop now" opens
 * the shop with ?flavour=<slug>, which GET /storefront/products accepts.
 *
 * Admin: CRUD at /admin/flavours. A flavour carries its products (productIds),
 * so mapping is done from the flavour, not the product form.
 */

type flavourDTOType struct {
	ID          string  `json:"id"`
	Name        string  `json:"name" example:"Chocolate"`
	Slug        string  `json:"slug" example:"chocolate"`
	Description *string `json:"description" example:"Rich sponges, drips and dark glazes for serious chocolate lovers."`
	// The card's photo (an /admin/images upload URL) and its alt text.
	ImageURL     *string `json:"imageUrl"`
	ImageAltText *string `json:"imageAltText"`
	// The card's tint, "#RRGGBB"; null lets the website choose.
	BackgroundColour *string `json:"backgroundColour" example:"#F8E8E8"`
	// Cards are shown in ascending sortOrder, then by name.
	SortOrder int `json:"sortOrder" example:"1"`
	// active | inactive. An inactive flavour is not shown on the website.
	Status string `json:"status" example:"active"`
	// Products mapped to the flavour. Storefront: active ones only.
	ProductCount int `json:"productCount" example:"6"`
	// Admin reads only: the mapped product ids.
	ProductIDs []string `json:"productIds,omitempty"`
	CreatedAt  string   `json:"createdAt,omitempty"`
	UpdatedAt  string   `json:"updatedAt,omitempty"`
}

func flavourDTO(f catalogapi.Flavour) flavourDTOType {
	out := flavourDTOType{
		ID: f.ID, Name: f.Name, Slug: f.Slug, Description: f.Description, ImageURL: f.ImageURL,
		ImageAltText: f.ImageAltText, BackgroundColour: f.BackgroundColour, SortOrder: f.SortOrder,
		Status: f.Status, ProductCount: f.ProductCount, ProductIDs: f.ProductIDs,
	}
	if !f.CreatedAt.IsZero() {
		out.CreatedAt = f.CreatedAt.Format(time.RFC3339)
		out.UpdatedAt = f.UpdatedAt.Format(time.RFC3339)
	}
	return out
}

func flavourDTOs(in []catalogapi.Flavour) []flavourDTOType {
	out := make([]flavourDTOType, 0, len(in))
	for _, f := range in {
		out = append(out, flavourDTO(f))
	}
	return out
}

type flavourRequest struct {
	Name             string  `json:"name" example:"Chocolate"`
	Description      *string `json:"description,omitempty" example:"Rich sponges, drips and dark glazes for serious chocolate lovers."`
	ImageURL         *string `json:"imageUrl,omitempty"`
	ImageAltText     *string `json:"imageAltText,omitempty"`
	BackgroundColour *string `json:"backgroundColour,omitempty" example:"#F8E8E8"`
	SortOrder        int     `json:"sortOrder" example:"1"`
	// active (default) | inactive
	Status string `json:"status,omitempty" example:"active"`
	// Replaces the mapped products. Omit on an update to leave them as they
	// are; [] unmaps them all.
	ProductIDs []string `json:"productIds,omitempty"`
}

func (req flavourRequest) toInput() domain.FlavourInput {
	return domain.FlavourInput{
		Name: req.Name, Description: req.Description, ImageURL: req.ImageURL, ImageAltText: req.ImageAltText,
		BackgroundColour: req.BackgroundColour, SortOrder: req.SortOrder, Status: req.Status, ProductIDs: req.ProductIDs,
	}
}

// @Summary		Flavours for the home page
// @Description	The "Discover our flavours" cards: active flavours with at least one active product, in sortOrder. List a flavour's products with GET /storefront/products?flavour=<slug>.
// @Tags			storefront
// @Produce		json
// @Success		200	{object}	httpx.Envelope{data=[]flavourDTOType}
// @Router			/storefront/flavours [get]
func (h *Handler) StorefrontFlavours(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.StorefrontFlavours(r.Context())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, flavourDTOs(out))
}

// @Summary		List flavours
// @Tags			admin
// @Produce		json
// @Security		BearerAuth
// @Param			status	query		string	false	"active | inactive (default all)"
// @Param			search	query		string	false	"Name contains"
// @Success		200		{object}	httpx.Envelope{data=[]flavourDTOType}
// @Router			/admin/flavours [get]
func (h *Handler) ListFlavours(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.ListFlavours(r.Context(), r.URL.Query().Get("status"), r.URL.Query().Get("search"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, flavourDTOs(out))
}

// @Summary		Get a flavour
// @Description	With its mapped productIds.
// @Tags			admin
// @Produce		json
// @Security		BearerAuth
// @Param			flavourId	path		string	true	"flavourId"
// @Success		200			{object}	httpx.Envelope{data=flavourDTOType}
// @Router			/admin/flavours/{flavourId} [get]
func (h *Handler) GetFlavour(w http.ResponseWriter, r *http.Request) {
	f, err := h.svc.GetFlavour(r.Context(), chi.URLParam(r, "flavourId"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, flavourDTO(*f))
}

// @Summary		Create a flavour
// @Description	A home page flavour card with its products. Upload the photo first (POST /admin/images/upload-url) and send its URL as imageUrl.
// @Tags			admin
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body		flavourRequest	true	"Request body"
// @Success		201		{object}	httpx.Envelope{data=flavourDTOType}
// @Failure		409		{object}	httpx.Envelope	"FLAVOUR_NAME_TAKEN"
// @Router			/admin/flavours [post]
func (h *Handler) CreateFlavour(w http.ResponseWriter, r *http.Request) {
	var req flavourRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	f, err := h.svc.CreateFlavour(r.Context(), staffClaims(r), req.toInput())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Created(w, flavourDTO(*f))
}

// @Summary		Update a flavour
// @Description	Replaces the flavour's fields (send them all). productIds replaces the mapped products when sent; omit it to leave them.
// @Tags			admin
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			flavourId	path		string			true	"flavourId"
// @Param			body		body		flavourRequest	true	"Request body"
// @Success		200			{object}	httpx.Envelope{data=flavourDTOType}
// @Failure		409			{object}	httpx.Envelope	"FLAVOUR_NAME_TAKEN"
// @Router			/admin/flavours/{flavourId} [put]
func (h *Handler) UpdateFlavour(w http.ResponseWriter, r *http.Request) {
	var req flavourRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	f, err := h.svc.UpdateFlavour(r.Context(), staffClaims(r), chi.URLParam(r, "flavourId"), req.toInput())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, flavourDTO(*f))
}

// @Summary		Delete a flavour
// @Description	Removes the flavour and unmaps its products. The products are kept.
// @Tags			admin
// @Produce		json
// @Security		BearerAuth
// @Param			flavourId	path		string	true	"flavourId"
// @Success		200			{object}	httpx.Envelope
// @Router			/admin/flavours/{flavourId} [delete]
func (h *Handler) DeleteFlavour(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteFlavour(r.Context(), staffClaims(r), chi.URLParam(r, "flavourId")); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"deleted": true})
}
