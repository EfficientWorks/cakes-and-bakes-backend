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

// shopBusinessDTO is what a shop shows about the business it belongs to: its
// name and logo, how it prices, and how to reach it. Nothing internal.
type shopBusinessDTO struct {
	Name         string     `json:"name" example:"Cakes & Bakes"`
	Slug         string     `json:"slug" example:"cakes-and-bakes"`
	LogoURL      *string    `json:"logoUrl"`
	WebsiteURL   *string    `json:"websiteUrl"`
	Currency     string     `json:"currency" example:"GBP"`
	Locale       string     `json:"locale" example:"en-GB"`
	Timezone     string     `json:"timezone" example:"Europe/London"`
	TaxMode      string     `json:"taxMode" example:"inclusive"`
	TaxName      string     `json:"taxName" example:"VAT"`
	ContactEmail *string    `json:"contactEmail"`
	ContactPhone *string    `json:"contactPhone"`
	Country      countryDTO `json:"country"`
}

// ShopBusiness godoc
//
//	@Summary		The shop's business
//	@Description	The business this shop belongs to — chosen by the X-Business header, the request's Origin (the business's registered website) or the server's STOREFRONT_BUSINESS — with its logo, currency, locale and country conventions. The website reads it once to brand itself.
//	@Tags			storefront
//	@Produce		json
//	@Param			X-Business	header		string	false	"Business slug"
//	@Success		200			{object}	httpx.Envelope{data=shopBusinessDTO}
//	@Failure		404			{object}	httpx.Envelope	"NOT_FOUND"
//	@Router			/storefront/business [get]
func (h *Handler) ShopBusiness(w http.ResponseWriter, r *http.Request) {
	id := tenant.BusinessID(r.Context())
	if id == "" {
		httpx.WriteError(w, r, apperr.NotFound("This shop is not linked to a business."))
		return
	}
	b, err := h.svc.GetBusiness(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	profile, _ := country.Lookup(b.CountryCode)
	// Public and the same for every visitor: browsers and CDNs may keep it.
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("Vary", "X-Business, Origin")
	httpx.OK(w, shopBusinessDTO{
		Name: b.Name, Slug: b.Slug, LogoURL: b.LogoURL, WebsiteURL: b.WebsiteURL,
		Currency: b.Currency, Locale: b.Locale, Timezone: b.Timezone, TaxMode: b.TaxMode, TaxName: b.TaxName,
		ContactEmail: b.ContactEmail, ContactPhone: b.ContactPhone, Country: toCountryDTO(profile),
	})
}
