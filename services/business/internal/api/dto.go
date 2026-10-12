package api

import (
	"time"

	"bakehouse/pkg/country"
	"bakehouse/services/business/businessapi"
	"bakehouse/services/identity/identityapi"
)

// --- Countries ------------------------------------------------------------------

type countryDTO struct {
	Code          string  `json:"code" example:"GB"`
	Name          string  `json:"name" example:"United Kingdom"`
	Currency      string  `json:"currency" example:"GBP"`
	Timezone      string  `json:"timezone" example:"Europe/London"`
	Locale        string  `json:"locale" example:"en-GB"`
	PhonePrefix   string  `json:"phonePrefix" example:"+44"`
	TaxMode       string  `json:"taxMode" example:"inclusive"`
	TaxName       string  `json:"taxName" example:"VAT"`
	TaxRatePct    float64 `json:"taxRatePct" example:"20"`
	DistanceUnit  string  `json:"distanceUnit" example:"mi"`
	PostcodeLabel string  `json:"postcodeLabel" example:"Postcode"`
}

func toCountryDTO(p country.Profile) countryDTO {
	return countryDTO{
		Code: p.Code, Name: p.Name, Currency: p.Currency, Timezone: p.Timezone, Locale: p.Locale,
		PhonePrefix: p.PhonePrefix, TaxMode: p.TaxMode, TaxName: p.TaxName, TaxRatePct: p.TaxRatePct,
		DistanceUnit: p.DistanceUnit, PostcodeLabel: p.PostcodeLabel,
	}
}

// --- Businesses -------------------------------------------------------------------

type businessDTO struct {
	ID                string  `json:"id" example:"5b0c6e9e-2f0a-4c55-9d8e-1f2a3b4c5d6e"`
	Name              string  `json:"name" example:"Cakes & Bakes"`
	Slug              string  `json:"slug" example:"cakes-and-bakes"`
	CountryCode       string  `json:"countryCode" example:"GB"`
	Currency          string  `json:"currency" example:"GBP"`
	Timezone          string  `json:"timezone" example:"Europe/London"`
	Locale            string  `json:"locale" example:"en-GB"`
	TaxMode           string  `json:"taxMode" example:"inclusive"`
	TaxName           string  `json:"taxName" example:"VAT"`
	DefaultTaxRatePct float64 `json:"defaultTaxRatePct" example:"20"`
	ContactEmail      *string `json:"contactEmail" example:"hello@cakesandbakes.co.uk"`
	ContactPhone      *string `json:"contactPhone" example:"+44 20 7946 0000"`
	LogoURL           *string `json:"logoUrl" example:"https://media.emzo.app/images/cakes-and-bakes-logo"`
	WebsiteURL        *string `json:"websiteUrl" example:"https://cakesandbakes.co.uk"`
	// active | suspended
	Status    string `json:"status" example:"active"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

func toBusinessDTO(b *businessapi.Business) businessDTO {
	return businessDTO{
		ID: b.ID, Name: b.Name, Slug: b.Slug, CountryCode: b.CountryCode, Currency: b.Currency,
		Timezone: b.Timezone, Locale: b.Locale, TaxMode: b.TaxMode, TaxName: b.TaxName,
		DefaultTaxRatePct: b.DefaultTaxRatePct, ContactEmail: b.ContactEmail, ContactPhone: b.ContactPhone,
		LogoURL: b.LogoURL, WebsiteURL: b.WebsiteURL,
		Status: b.Status, CreatedAt: b.CreatedAt.Format(time.RFC3339), UpdatedAt: b.UpdatedAt.Format(time.RFC3339),
	}
}

// registerBusinessRequest registers a business. Only name and countryCode are
// required; every other setting defaults from the country (see GET
// /platform/countries).
type registerBusinessRequest struct {
	Name string `json:"name" example:"Cakes & Bakes"`
	// GB, US or IN.
	CountryCode string `json:"countryCode" example:"GB"`
	// Omitted: made from the name ("cakes-bakes").
	Slug         string `json:"slug,omitempty" example:"cakes-and-bakes"`
	ContactEmail string `json:"contactEmail,omitempty" example:"hello@cakesandbakes.co.uk"`
	ContactPhone string `json:"contactPhone,omitempty" example:"+44 20 7946 0000"`
	// Overrides of the country defaults. Omit to use the default.
	Currency          string   `json:"currency,omitempty" example:"GBP"`
	Timezone          string   `json:"timezone,omitempty" example:"Europe/London"`
	Locale            string   `json:"locale,omitempty" example:"en-GB"`
	TaxMode           string   `json:"taxMode,omitempty" example:"inclusive"`
	TaxName           string   `json:"taxName,omitempty" example:"VAT"`
	DefaultTaxRatePct *float64 `json:"defaultTaxRatePct,omitempty" example:"20"`
	// The logo on the shop, emails and invoices: an https:// image address
	// (upload one with POST /platform/media/upload-ticket).
	LogoURL string `json:"logoUrl,omitempty" example:"https://media.emzo.app/images/cakes-and-bakes-logo"`
	// Where the business's shop is served from. Email links point here.
	WebsiteURL string `json:"websiteUrl,omitempty" example:"https://cakesandbakes.co.uk"`
}

func (r registerBusinessRequest) toModel() businessapi.NewBusiness {
	return businessapi.NewBusiness{
		Name: r.Name, Slug: r.Slug, CountryCode: r.CountryCode, ContactEmail: r.ContactEmail,
		ContactPhone: r.ContactPhone, Currency: r.Currency, Timezone: r.Timezone, Locale: r.Locale,
		TaxMode: r.TaxMode, TaxName: r.TaxName, DefaultTaxRatePct: r.DefaultTaxRatePct,
		LogoURL: r.LogoURL, WebsiteURL: r.WebsiteURL,
	}
}

// updateBusinessRequest changes some settings. Omitted fields are left as
// they are; the country cannot be changed.
type updateBusinessRequest struct {
	Name              *string  `json:"name,omitempty"`
	ContactEmail      *string  `json:"contactEmail,omitempty"`
	ContactPhone      *string  `json:"contactPhone,omitempty"`
	Currency          *string  `json:"currency,omitempty"`
	Timezone          *string  `json:"timezone,omitempty"`
	Locale            *string  `json:"locale,omitempty"`
	TaxMode           *string  `json:"taxMode,omitempty"`
	TaxName           *string  `json:"taxName,omitempty"`
	DefaultTaxRatePct *float64 `json:"defaultTaxRatePct,omitempty"`
	// "" removes the logo / website.
	LogoURL    *string `json:"logoUrl,omitempty"`
	WebsiteURL *string `json:"websiteUrl,omitempty"`
	// active | suspended
	Status *string `json:"status,omitempty" example:"active"`
}

func (r updateBusinessRequest) toModel() businessapi.BusinessPatch {
	return businessapi.BusinessPatch{
		Name: r.Name, ContactEmail: r.ContactEmail, ContactPhone: r.ContactPhone, Currency: r.Currency,
		Timezone: r.Timezone, Locale: r.Locale, TaxMode: r.TaxMode, TaxName: r.TaxName,
		DefaultTaxRatePct: r.DefaultTaxRatePct, Status: r.Status,
		LogoURL: r.LogoURL, WebsiteURL: r.WebsiteURL,
	}
}

// --- Owner login ----------------------------------------------------------------

// createOwnerRequest is the business's first admin login.
type createOwnerRequest struct {
	Email string `json:"email" example:"owner@cakesandbakes.co.uk"`
	// At least 12 characters.
	Password  string `json:"password" example:"correct-horse-battery"`
	FirstName string `json:"firstName" example:"Priya"`
	LastName  string `json:"lastName" example:"Shah"`
}

type ownerDTO struct {
	StaffID    string `json:"staffId"`
	BusinessID string `json:"businessId"`
	Email      string `json:"email"`
	FirstName  string `json:"firstName"`
	LastName   string `json:"lastName"`
	// Always super_admin: the owner manages the whole business.
	Role string `json:"role" example:"super_admin"`
}

func toOwnerDTO(businessID string, u *identityapi.StaffUser) ownerDTO {
	return ownerDTO{StaffID: u.ID, BusinessID: businessID, Email: u.Email, FirstName: u.FirstName, LastName: u.LastName, Role: "super_admin"}
}
