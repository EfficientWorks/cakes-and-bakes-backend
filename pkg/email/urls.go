package email

import (
	"fmt"
	"strings"
)

// Where the two front-ends live, for the links inside an email.
//
// Package-level rather than threaded through every template call: a template
// is chosen by name and rendered from a map of strings, so there is no
// natural place to pass configuration down. These are set once at startup,
// before anything is sent.
//
// The defaults are the local ones. Deployments must set STOREFRONT_BASE_URL
// and ADMIN_BASE_URL — a link to localhost in a real email is obviously
// wrong, which is better than a link to a domain that merely looks right and
// does not resolve, which is what was being sent.
var (
	storefrontURL = "http://localhost:3000"
	adminURL      = "http://localhost:3001"
)

// The domains the templates are written against. Every rendered email has
// these swapped for the configured origins on the way out, which keeps the
// template bodies readable rather than a run of format verbs.
const (
	storefrontPlaceholder = "https://shop.invalid"
	adminPlaceholder      = "https://admin.invalid"
)

// SetBaseURLs points the email templates at the running front-ends.
func SetBaseURLs(storefront, admin string) {
	if s := strings.TrimRight(strings.TrimSpace(storefront), "/"); s != "" {
		storefrontURL = s
	}
	if a := strings.TrimRight(strings.TrimSpace(admin), "/"); a != "" {
		adminURL = a
	}
}

// ShopLink is a link to path on the sending business's own shop. It is
// written against the placeholder and resolved when the email is rendered
// (its business's website, else STOREFRONT_BASE_URL).
func ShopLink(path string) string { return storefrontPlaceholder + path }

// StorefrontURL is where a customer is sent.
func StorefrontURL() string { return storefrontURL }

// AdminURL is where a member of staff is sent.
func AdminURL() string { return adminURL }

// applyBaseURLs rewrites the placeholder domains in a rendered email.
//
// Admin first: the staff domain contains the storefront one as a suffix, so
// replacing the storefront first would leave "https://admin." glued to the
// wrong origin.
func applyBaseURLs(e *RenderedEmail) *RenderedEmail {
	if e == nil {
		return nil
	}
	fix := func(s string) string {
		s = strings.ReplaceAll(s, adminPlaceholder, adminURL)
		return strings.ReplaceAll(s, storefrontPlaceholder, storefrontURL)
	}
	e.Subject = fix(e.Subject)
	e.HTMLBody = fix(e.HTMLBody)
	e.TextBody = fix(e.TextBody)
	return e
}

// Money renders minor units as a person reads them.
//
// Every amount in this codebase is an integer number of cents, which is
// right everywhere except the one place a human looks at it. Printing the
// raw value put "22000" on an order confirmation for a $220.00 order.
func Money(cents int64) string {
	neg := ""
	if cents < 0 {
		neg = "-"
		cents = -cents
	}
	// The symbol is filled in by applyBrand, from the business's currency.
	return fmt.Sprintf("%s%s%d.%02d", neg, moneyMark, cents/100, cents%100)
}
