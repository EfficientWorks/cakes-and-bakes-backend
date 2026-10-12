package email

import (
	"context"
	"fmt"
	"html/template"
	"strings"

	"bakehouse/pkg/money"
	"bakehouse/pkg/tenant"
)

/*
 * Whose email it is.
 *
 * Every email goes out as the business it is about — its name in the
 * subject, header and footer, its logo in the band, links to its own shop,
 * amounts in its currency. The templates are written once against
 * placeholders (the business name {{business}}, the shop domain, the brandLogo and
 * moneyMark tokens) and Brand fills them in after rendering, the same way
 * applyBaseURLs fixes the domains.
 *
 * The business is the one on the email (Email.BusinessID) or, when that is
 * empty, the one the request acts for (tenant.BusinessID) — every send runs
 * inside a staff or storefront request, so the context already knows it.
 * Without either, the email goes out as the platform (PlatformBrand).
 */

// Brand is how an email presents the business that sends it.
type Brand struct {
	Name string
	// LogoURL is an https image; empty sets the name in type instead.
	LogoURL string
	// WebsiteURL is the business's shop; empty uses STOREFRONT_BASE_URL.
	WebsiteURL string
	// Currency is the ISO code amounts are shown in.
	Currency string
}

// PlatformBrand is used when an email belongs to no business.
var PlatformBrand = Brand{Name: "emzo Bakehouse", Currency: "GBP"}

// BrandLookup returns the brand of a business; ok is false when it cannot.
type BrandLookup func(ctx context.Context, businessID string) (b Brand, ok bool)

var brandLookup BrandLookup

// SetBrandLookup installs how a business id becomes a Brand. Called once at
// startup; the lookup is expected to cache.
func SetBrandLookup(fn BrandLookup) { brandLookup = fn }

// BrandFor is the brand an email is sent under.
func BrandFor(ctx context.Context, e Email) Brand {
	id := e.BusinessID
	if id == "" {
		id = tenant.BusinessID(ctx)
	}
	if id != "" && brandLookup != nil {
		if b, ok := brandLookup(ctx, id); ok {
			if b.Name == "" {
				b.Name = PlatformBrand.Name
			}
			if b.Currency == "" {
				b.Currency = PlatformBrand.Currency
			}
			return b
		}
	}
	b := PlatformBrand
	b.LogoURL = logoURL
	return b
}

// Placeholders the templates are written against.
const (
	brandNamePlaceholder = "{{business}}"
	// brandLogo marks where the header logo goes.
	brandLogo = "<!--brand-logo-->"
	// moneyMark stands for the currency symbol in an amount (¤, the generic
	// currency sign).
	moneyMark = "¤"
)

// RenderBranded renders a template as brand sends it.
func RenderBranded(brand Brand, templateAlias string, data map[string]interface{}) (*RenderedEmail, error) {
	e, err := renderTemplate(templateAlias, data)
	if err != nil {
		return nil, err
	}
	return applyBaseURLs(applyBrand(e, brand)), nil
}

// applyBrand fills the brand placeholders in a rendered email.
func applyBrand(e *RenderedEmail, b Brand) *RenderedEmail {
	if e == nil {
		return nil
	}
	symbol := money.Symbol(b.Currency)
	plain := strings.NewReplacer(brandNamePlaceholder, b.Name, moneyMark, symbol)
	html := strings.NewReplacer(
		brandLogo, logoHTML(b),
		brandNamePlaceholder, template.HTMLEscapeString(b.Name),
		moneyMark, template.HTMLEscapeString(symbol),
	)
	e.Subject = plain.Replace(e.Subject)
	e.TextBody = plain.Replace(e.TextBody)
	e.HTMLBody = html.Replace(e.HTMLBody)
	if site := strings.TrimRight(b.WebsiteURL, "/"); site != "" {
		// The business's own shop, before applyBaseURLs would point the
		// links at the platform default.
		e.HTMLBody = strings.ReplaceAll(e.HTMLBody, storefrontPlaceholder, site)
		e.TextBody = strings.ReplaceAll(e.TextBody, storefrontPlaceholder, site)
	}
	return e
}

// logoHTML is the header logo: the business's image when it has one,
// otherwise its name set in type, so the band never shows a broken image.
// The alt text carries the name for clients that block images.
func logoHTML(b Brand) string {
	name := template.HTMLEscapeString(b.Name)
	if b.LogoURL == "" {
		return `<span style="display:inline-block;color:#ffffff;font-size:22px;line-height:1.2;font-weight:700;letter-spacing:1px;">` + name + `</span>`
	}
	return fmt.Sprintf(`<img src="%s" width="150" alt="%s" style="display:block;width:150px;max-width:150px;height:auto;border:0;outline:none;text-decoration:none;color:#ffffff;font-size:22px;font-weight:700;letter-spacing:1px;">`,
		template.HTMLEscapeString(b.LogoURL), name)
}
