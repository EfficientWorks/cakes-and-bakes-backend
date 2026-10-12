package geo

import (
	"context"
	"strings"
)

/*
 * The country an address is searched in.
 *
 * A postcode means nothing without one — "E1 6AN" is London, "10002" is
 * Manhattan — so every search is limited to the country of the business the
 * request is for, and the name is appended to the text searched ("E1 6AN,
 * United Kingdom"). The business is known from the request; how it becomes a
 * country is installed once at startup (SetCountryLookup).
 */

// DefaultCountry is searched when the request's business is unknown: the
// product's launch market.
const DefaultCountry = "GB"

// searchNames is how each supported country is written at the end of a
// search, as the geocoders understand it best.
var searchNames = map[string]string{
	"GB": "United Kingdom",
	"US": "USA",
	"IN": "India",
}

var countryLookup func(ctx context.Context) string

// SetCountryLookup installs how a request's country (ISO alpha-2) is found.
func SetCountryLookup(fn func(ctx context.Context) string) { countryLookup = fn }

// CountryOf is the ISO code address searches for this request are limited
// to: the business's country, else DefaultCountry.
func CountryOf(ctx context.Context) string {
	if countryLookup != nil {
		if c := strings.ToUpper(strings.TrimSpace(countryLookup(ctx))); searchNames[c] != "" {
			return c
		}
	}
	return DefaultCountry
}

// inCountry appends the request's country to a search: "E1 6AN, United Kingdom".
func inCountry(ctx context.Context, q string) string {
	return q + ", " + searchNames[CountryOf(ctx)]
}
