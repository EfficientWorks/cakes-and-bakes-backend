// Package country holds what differs between the countries emzo Bakehouse is
// sold in: currency, timezone, language, tax and the shape of a phone number
// or postcode. It is the one place to add a country; code elsewhere reads a
// Profile instead of testing for "GB" or "US".
package country

import (
	"regexp"
	"sort"
	"strings"
)

// TaxMode says whether shelf prices already include tax.
const (
	TaxInclusive = "inclusive" // UK VAT, India GST: the price shown is the price paid
	TaxExclusive = "exclusive" // US sales tax: added on top at checkout
)

// Profile is one country's defaults. A business copies them when it is
// registered and may change its own copy afterwards.
type Profile struct {
	Code          string  // ISO 3166-1 alpha-2
	Name          string  // display name
	Currency      string  // ISO 4217
	Timezone      string  // IANA, the business default (stores may differ)
	Locale        string  // BCP 47, for dates and number formatting
	PhonePrefix   string  // international dialling code
	TaxMode       string  // TaxInclusive or TaxExclusive
	TaxName       string  // what receipts call the tax
	TaxRatePct    float64 // the standard rate; stores and products may differ
	DistanceUnit  string  // "mi" or "km"
	PostcodeLabel string  // what forms call the postcode
	postcode      *regexp.Regexp
}

// ValidPostcode reports whether v looks like a postcode in this country.
// Spacing and case are ignored.
func (p Profile) ValidPostcode(v string) bool {
	return p.postcode.MatchString(strings.ToUpper(strings.TrimSpace(v)))
}

var profiles = map[string]Profile{
	"GB": {
		Code: "GB", Name: "United Kingdom", Currency: "GBP", Timezone: "Europe/London", Locale: "en-GB",
		PhonePrefix: "+44", TaxMode: TaxInclusive, TaxName: "VAT", TaxRatePct: 20,
		DistanceUnit: "mi", PostcodeLabel: "Postcode",
		// Royal Mail format: outward code, optional space, inward code.
		postcode: regexp.MustCompile(`^[A-Z]{1,2}[0-9][A-Z0-9]? ?[0-9][A-Z]{2}$`),
	},
	"US": {
		Code: "US", Name: "United States", Currency: "USD", Timezone: "America/New_York", Locale: "en-US",
		PhonePrefix: "+1", TaxMode: TaxExclusive, TaxName: "Sales tax", TaxRatePct: 0,
		DistanceUnit: "mi", PostcodeLabel: "ZIP code",
		postcode: regexp.MustCompile(`^[0-9]{5}(-[0-9]{4})?$`),
	},
	"IN": {
		Code: "IN", Name: "India", Currency: "INR", Timezone: "Asia/Kolkata", Locale: "en-IN",
		PhonePrefix: "+91", TaxMode: TaxInclusive, TaxName: "GST", TaxRatePct: 5,
		DistanceUnit: "km", PostcodeLabel: "PIN code",
		postcode: regexp.MustCompile(`^[1-9][0-9]{5}$`),
	},
}

// Lookup returns the profile for an ISO country code, case-insensitively.
func Lookup(code string) (Profile, bool) {
	p, ok := profiles[strings.ToUpper(strings.TrimSpace(code))]
	return p, ok
}

// All returns every supported country, sorted by code.
func All() []Profile {
	out := make([]Profile, 0, len(profiles))
	for _, p := range profiles {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}
