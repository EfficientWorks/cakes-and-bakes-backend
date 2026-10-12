package domain

import (
	"net/url"
	"regexp"
	"strings"
	"time"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/country"
)

var currencyCode = regexp.MustCompile(`^[A-Z]{3}$`)

// validateSettings checks the settings every business must have valid,
// whether they came from the country or from the caller.
func validateSettings(currency, timezone, taxMode string, taxRatePct float64) error {
	var fields []apperr.FieldError
	if !currencyCode.MatchString(currency) {
		fields = append(fields, apperr.FieldError{Field: "currency", Code: "INVALID", Message: "Use a 3-letter ISO code, e.g. GBP."})
	}
	if _, err := time.LoadLocation(timezone); err != nil || timezone == "" {
		fields = append(fields, apperr.FieldError{Field: "timezone", Code: "INVALID", Message: "Use an IANA timezone, e.g. Europe/London."})
	}
	if taxMode != country.TaxInclusive && taxMode != country.TaxExclusive {
		fields = append(fields, apperr.FieldError{Field: "taxMode", Code: "INVALID", Message: "Use inclusive or exclusive."})
	}
	if taxRatePct < 0 || taxRatePct > 100 {
		fields = append(fields, apperr.FieldError{Field: "defaultTaxRatePct", Code: "OUT_OF_RANGE", Message: "Between 0 and 100."})
	}
	if len(fields) > 0 {
		return apperr.Validation("Check the business settings.").WithFields(fields...)
	}
	return nil
}

func unsupportedCountry(code string) error {
	codes := make([]string, 0, 3)
	for _, p := range country.All() {
		codes = append(codes, p.Code)
	}
	return fieldError("countryCode", "UNSUPPORTED",
		"\""+code+"\" is not a supported country. Use one of: "+strings.Join(codes, ", ")+".")
}

func fieldError(field, code, msg string) error {
	return apperr.Validation(msg).WithFields(apperr.FieldError{Field: field, Code: code, Message: msg})
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify turns a name into a URL-safe handle: "Cakes & Bakes Ltd" becomes
// "cakes-bakes-ltd".
func Slugify(v string) string {
	return strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(strings.TrimSpace(v)), "-"), "-")
}

func orDefault(v, def string) string {
	if v = strings.TrimSpace(v); v == "" {
		return def
	}
	return v
}

func nilIfBlank(v string) *string {
	if v = strings.TrimSpace(v); v == "" {
		return nil
	}
	return &v
}

func pick(p *string, current string) string {
	if p == nil {
		return current
	}
	return *p
}

func pickFloat(p *float64, current float64) float64 {
	if p == nil {
		return current
	}
	return *p
}

// validateBranding checks a logo URL and a website, and returns the website
// reduced to its origin ("https://cakesandbakes.co.uk"). Empty values pass
// through as empty.
//
// Only absolute https URLs: both end up in emails and on the shop, so a
// javascript: or data: URL, or plain http, must never be stored. http is
// allowed for localhost so a shop can be tried out locally.
func validateBranding(logoURL, websiteURL string) (logo, website string, err error) {
	var fields []apperr.FieldError
	logo = strings.TrimSpace(logoURL)
	if logo != "" {
		if u, ok := safeURL(logo); !ok {
			fields = append(fields, apperr.FieldError{Field: "logoUrl", Code: "INVALID", Message: "Use an https:// image address."})
		} else {
			logo = u.String()
		}
	}
	website = strings.TrimSpace(websiteURL)
	if website != "" {
		if u, ok := safeURL(website); !ok {
			fields = append(fields, apperr.FieldError{Field: "websiteUrl", Code: "INVALID", Message: "Use the shop's https:// address, e.g. https://cakesandbakes.co.uk."})
		} else {
			website = u.Scheme + "://" + strings.ToLower(u.Host)
		}
	}
	if len(fields) > 0 {
		return "", "", apperr.Validation("Check the logo and website addresses.").WithFields(fields...)
	}
	return logo, website, nil
}

func safeURL(raw string) (*url.URL, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || len(raw) > 2048 {
		return nil, false
	}
	host := u.Hostname()
	local := host == "localhost" || host == "127.0.0.1"
	return u, u.Scheme == "https" || (u.Scheme == "http" && local)
}

func websiteTaken() error {
	return apperr.New("BUSINESS_WEBSITE_TAKEN", apperr.CategoryConflict,
		"Another business already uses that website address.").
		WithFields(apperr.FieldError{Field: "websiteUrl", Code: "TAKEN", Message: "Already in use."})
}
