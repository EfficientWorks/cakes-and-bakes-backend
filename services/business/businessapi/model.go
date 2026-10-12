// Package businessapi is the business service's published contract: the
// Business record other services may read. It holds no logic.
package businessapi

import "time"

// Business statuses.
const (
	StatusActive    = "active"
	StatusSuspended = "suspended"
)

// Business is one company using emzo Bakehouse — for example a bakery chain
// with several stores. Its country decides the defaults below when it is
// registered; each can be changed afterwards.
type Business struct {
	ID                string
	Name              string
	Slug              string
	CountryCode       string
	Currency          string
	Timezone          string
	Locale            string
	TaxMode           string // country.TaxInclusive or country.TaxExclusive
	TaxName           string
	DefaultTaxRatePct float64
	ContactEmail      *string
	ContactPhone      *string
	// LogoURL is the logo on the business's shop, emails and invoices.
	LogoURL *string
	// WebsiteURL is the origin the business's shop is served from
	// (https://cakesandbakes.co.uk). Email links point there, and a browser
	// request from it is recognised as this business's shop.
	WebsiteURL *string
	Status     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// NewBusiness is what registering a business needs. Optional fields left
// empty take the country's default.
type NewBusiness struct {
	Name              string
	Slug              string
	CountryCode       string
	ContactEmail      string
	ContactPhone      string
	Currency          string
	Timezone          string
	Locale            string
	TaxMode           string
	TaxName           string
	DefaultTaxRatePct *float64
	LogoURL           string
	WebsiteURL        string
}

// BusinessPatch changes some of a business's settings. Nil leaves a field as
// it is. The country is fixed once registered.
type BusinessPatch struct {
	Name              *string
	ContactEmail      *string
	ContactPhone      *string
	Currency          *string
	Timezone          *string
	Locale            *string
	TaxMode           *string
	TaxName           *string
	DefaultTaxRatePct *float64
	// LogoURL and WebsiteURL: "" removes the value.
	LogoURL    *string
	WebsiteURL *string
	Status     *string
}
