package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/httpx"
	"bakehouse/pkg/media"
	"bakehouse/services/business/internal/domain"
)

// Handler serves the platform API: the emzo team registering and managing
// the businesses that use emzo Bakehouse.
type Handler struct {
	svc *domain.Service
	// media stores uploaded logos; nil until UseMedia.
	media media.Storer
}

func NewHandler(svc *domain.Service) *Handler { return &Handler{svc: svc} }

// ListCountries godoc
//
//	@Summary		Supported countries
//	@Description	The countries a business can be registered in, with the defaults each one gives a new business.
//	@Tags			platform
//	@Produce		json
//	@Security		PlatformKey
//	@Success		200	{object}	httpx.Envelope{data=[]countryDTO}
//	@Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
//	@Router			/platform/countries [get]
func (h *Handler) ListCountries(w http.ResponseWriter, r *http.Request) {
	out := []countryDTO{}
	for _, p := range h.svc.Countries() {
		out = append(out, toCountryDTO(p))
	}
	httpx.OK(w, out)
}

// RegisterBusiness godoc
//
//	@Summary		Register a business
//	@Description	Creates a business. Only name and countryCode are required: the country fills currency, timezone, locale and tax (GB: GBP, Europe/London, en-GB, VAT 20% included in prices). Next, give it a login with POST /platform/businesses/{businessId}/owner.
//	@Tags			platform
//	@Accept			json
//	@Produce		json
//	@Security		PlatformKey
//	@Param			body	body		registerBusinessRequest	true	"The business"
//	@Success		201		{object}	httpx.Envelope{data=businessDTO}
//	@Failure		400		{object}	httpx.Envelope	"VALIDATION_FAILED"
//	@Failure		401		{object}	httpx.Envelope	"UNAUTHENTICATED"
//	@Failure		409		{object}	httpx.Envelope	"BUSINESS_SLUG_TAKEN"
//	@Router			/platform/businesses [post]
func (h *Handler) RegisterBusiness(w http.ResponseWriter, r *http.Request) {
	var req registerBusinessRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	b, err := h.svc.RegisterBusiness(r.Context(), req.toModel())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Created(w, toBusinessDTO(b))
}

// ListBusinesses godoc
//
//	@Summary		List businesses
//	@Description	Every registered business, newest first.
//	@Tags			platform
//	@Produce		json
//	@Security		PlatformKey
//	@Param			search	query		string	false	"Matches name or slug"
//	@Param			status	query		string	false	"active | suspended"
//	@Success		200		{object}	httpx.Envelope{data=[]businessDTO}
//	@Failure		401		{object}	httpx.Envelope	"UNAUTHENTICATED"
//	@Router			/platform/businesses [get]
func (h *Handler) ListBusinesses(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.ListBusinesses(r.Context(), r.URL.Query().Get("search"), r.URL.Query().Get("status"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out := make([]businessDTO, 0, len(list))
	for i := range list {
		out = append(out, toBusinessDTO(&list[i]))
	}
	httpx.OK(w, out)
}

// GetBusiness godoc
//
//	@Summary		Get a business
//	@Tags			platform
//	@Produce		json
//	@Security		PlatformKey
//	@Param			businessId	path		string	true	"Business id"
//	@Success		200			{object}	httpx.Envelope{data=businessDTO}
//	@Failure		401			{object}	httpx.Envelope	"UNAUTHENTICATED"
//	@Failure		404			{object}	httpx.Envelope	"NOT_FOUND"
//	@Router			/platform/businesses/{businessId} [get]
func (h *Handler) GetBusiness(w http.ResponseWriter, r *http.Request) {
	b, err := h.svc.GetBusiness(r.Context(), chi.URLParam(r, "businessId"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, toBusinessDTO(b))
}

// UpdateBusiness godoc
//
//	@Summary		Change a business's settings
//	@Description	Partial update: omitted fields keep their value. The country cannot be changed. status=suspended stops the business adding new logins.
//	@Tags			platform
//	@Accept			json
//	@Produce		json
//	@Security		PlatformKey
//	@Param			businessId	path		string					true	"Business id"
//	@Param			body		body		updateBusinessRequest	true	"Fields to change"
//	@Success		200			{object}	httpx.Envelope{data=businessDTO}
//	@Failure		400			{object}	httpx.Envelope	"VALIDATION_FAILED"
//	@Failure		401			{object}	httpx.Envelope	"UNAUTHENTICATED"
//	@Failure		404			{object}	httpx.Envelope	"NOT_FOUND"
//	@Router			/platform/businesses/{businessId} [patch]
func (h *Handler) UpdateBusiness(w http.ResponseWriter, r *http.Request) {
	var req updateBusinessRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	b, err := h.svc.UpdateBusiness(r.Context(), chi.URLParam(r, "businessId"), req.toModel())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, toBusinessDTO(b))
}

// CreateOwner godoc
//
//	@Summary		Create the business's admin login
//	@Description	Creates a super admin for the business. They sign in to the admin console with this email and password and set up stores, staff and the menu.
//	@Tags			platform
//	@Accept			json
//	@Produce		json
//	@Security		PlatformKey
//	@Param			businessId	path		string				true	"Business id"
//	@Param			body		body		createOwnerRequest	true	"The owner's login"
//	@Success		201			{object}	httpx.Envelope{data=ownerDTO}
//	@Failure		400			{object}	httpx.Envelope	"VALIDATION_FAILED"
//	@Failure		401			{object}	httpx.Envelope	"UNAUTHENTICATED"
//	@Failure		404			{object}	httpx.Envelope	"NOT_FOUND"
//	@Failure		409			{object}	httpx.Envelope	"EMAIL_ALREADY_EXISTS"
//	@Failure		422			{object}	httpx.Envelope	"BUSINESS_SUSPENDED"
//	@Router			/platform/businesses/{businessId}/owner [post]
func (h *Handler) CreateOwner(w http.ResponseWriter, r *http.Request) {
	var req createOwnerRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	businessID := chi.URLParam(r, "businessId")
	owner, err := h.svc.CreateOwner(r.Context(), businessID, req.Email, req.Password, req.FirstName, req.LastName)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Created(w, toOwnerDTO(businessID, owner))
}
