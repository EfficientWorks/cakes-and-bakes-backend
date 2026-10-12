package api

import (
	"net/http"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/country"
	"bakehouse/pkg/httpx"
	"bakehouse/pkg/tenant"
)

// myBusinessDTO is the signed-in staff member's business with its country's
// conventions, which the console formats money, dates, phone numbers and
// postcodes by.
type myBusinessDTO struct {
	businessDTO
	Country countryDTO `json:"country"`
}

// MyBusiness godoc
//
//	@Summary		The signed-in business
//	@Description	The business the caller's staff account belongs to, with its country's currency, locale, tax and postcode conventions. Read-only: the emzo team changes a business through the platform API.
//	@Tags			admin
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	httpx.Envelope{data=myBusinessDTO}
//	@Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
//	@Failure		404	{object}	httpx.Envelope	"NOT_FOUND — the account belongs to no business"
//	@Router			/admin/business [get]
func (h *Handler) MyBusiness(w http.ResponseWriter, r *http.Request) {
	id := tenant.BusinessID(r.Context())
	if id == "" {
		httpx.WriteError(w, r, apperr.NotFound("This account does not belong to a business."))
		return
	}
	b, err := h.svc.GetBusiness(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	profile, _ := country.Lookup(b.CountryCode)
	httpx.OK(w, myBusinessDTO{businessDTO: toBusinessDTO(b), Country: toCountryDTO(profile)})
}
