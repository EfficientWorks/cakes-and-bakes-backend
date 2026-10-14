package email

import (
	"fmt"
	"html/template"
	"strings"
)

/*
 * The one look every email shares.
 *
 * A deep-purple band with the wordmark, a white card that starts inside the
 * band and runs out of it, on a pale blue page; the brand colours the
 * storefront uses. Tables and inline styles throughout, because that is what
 * mail clients render; the overlap is a background gradient on the card's
 * cell, which the clients that cannot draw it (old Outlook) simply show as a
 * flat purple band above the card.
 *
 * Every template builds its body and hands it here, so the brand is changed
 * in one place. The logo is an image at a fixed path on the storefront —
 * see SetLogoURL — with the wordmark as alt text for clients that block
 * images.
 */

// Brand colours, as the storefront has them.
const (
	brandPurple = "#3B1245"
	brandInk    = "#1B1B3A"
	brandMuted  = "#5C5C7A"
	brandPage   = "#E9F3FB"
	// brandShell is the near-white container the band, card and footer sit
	// in, on top of the pale blue page (Figma: #F7F8FA).
	brandShell     = "#F7F8FA"
	brandPink      = "#FBE2EF"
	brandPinkLine  = "#F3C6DF"
	brandLine      = "#E6E6EF"
	brandRed       = "#F0506E"
	brandViolet    = "#B04AE0"
	brandGreen     = "#16A34A"
	brandCardWidth = 560
)

// logoURL is where the white wordmark is loaded from. Resolved by
// SetLogoURL/SetAssetBaseURL; empty means "no image to point at", and the
// header falls back to a text wordmark rather than a broken-image icon.
var (
	logoURL         string
	logoURLExplicit bool
)

// SetLogoURL overrides where the header wordmark is loaded from
// (EMAIL_LOGO_URL). An explicit URL always wins over the served default.
func SetLogoURL(u string) {
	if u = strings.TrimSpace(u); u != "" {
		logoURL = u
		logoURLExplicit = true
	}
}

// SetAssetBaseURL tells the templates where this API is publicly reachable
// (PUBLIC_BASE_URL), so the header can load the logo the API serves itself at
// LogoPath. It used to default onto the storefront origin, which does not
// host the file — every email went out with a broken image.
func SetAssetBaseURL(publicBaseURL string) {
	if logoURLExplicit {
		return
	}
	base := strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
	if base == "" || !HasEmbeddedLogo() {
		logoURL = ""
		return
	}
	logoURL = base + LogoPath
}

// headerLogo is the wordmark in the purple band: the image when there is one
// to load, otherwise the name set in type so the header never shows a broken
// image. The alt text carries the name for clients that block images.
//
// It is a placeholder here: applyBrand puts in the sending business's logo
// (see logoHTML).
func headerLogo() string { return brandLogo }

// shellOptions are the few knobs a template turns on the shared layout.
type shellOptions struct {
	// Title goes in <title> and is what a client shows in the tab or preview.
	Title string
	// HeaderRight is optional HTML placed on the right of the purple band —
	// the "2 items" pill on an order confirmation.
	HeaderRight string
}

// shell wraps a card body in the brand layout.
//
// Layout, per the Figma: pale blue page → a near-white container (square
// corners) → a square-cornered purple band with the wordmark → the white card,
// overlapping the bottom of the band → the footer, still inside the container.
func shell(opts shellOptions, body string) string {
	right := ""
	if opts.HeaderRight != "" {
		right = fmt.Sprintf(`<td align="right" valign="middle" style="padding:0;">%s</td>`, opts.HeaderRight)
	}
	logo := headerLogo()
	logoAlign := "center"
	logoWrap := `<table role="presentation" cellpadding="0" cellspacing="0" border="0" align="center"><tr><td align="center">` + logo + `</td></tr></table>`
	if right != "" {
		logoAlign = "left"
		logoWrap = logo
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1.0">
<meta name="x-apple-disable-message-reformatting">
<title>%s</title>
</head>
<body style="margin:0;padding:0;background-color:%s;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;-webkit-font-smoothing:antialiased;">
<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0" bgcolor="%s" style="background-color:%s;">
  <tr>
    <td align="center" style="padding:24px 12px 32px;">
      <!-- outer container: near-white, square corners -->
      <table role="presentation" width="%d" cellpadding="0" cellspacing="0" border="0" bgcolor="%s" style="width:%dpx;max-width:100%%;background-color:%s;">
        <!-- purple band with the wordmark (square corners) -->
        <tr>
          <td bgcolor="%s" style="background-color:%s;padding:26px 28px 18px;">
            <table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0">
              <tr>
                <td align="%s" valign="middle" style="padding:0;">%s</td>
                %s
              </tr>
            </table>
          </td>
        </tr>
        <!-- the card, starting inside the band -->
        <tr>
          <td bgcolor="%s" style="padding:0 20px;background-color:%s;background-image:linear-gradient(to bottom,%s 0,%s 96px,%s 96px,%s 100%%);">
            <table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0" bgcolor="#ffffff" style="background-color:#ffffff;border-radius:10px;box-shadow:0 6px 18px rgba(27,27,58,0.10);">
              <tr>
                <td bgcolor="#ffffff" style="padding:34px 32px 30px;background-color:#ffffff;color:%s;">
%s
                </td>
              </tr>
            </table>
          </td>
        </tr>
        <!-- footer, inside the container -->
        <tr>
          <td align="center" bgcolor="%s" style="padding:22px 20px 24px;background-color:%s;font-size:12px;color:%s;">&copy; {{business}}. All rights reserved.</td>
        </tr>
      </table>
    </td>
  </tr>
</table>
</body>
</html>`,
		template.HTMLEscapeString(opts.Title), brandPage, brandPage, brandPage,
		brandCardWidth, brandShell, brandCardWidth, brandShell,
		brandPurple, brandPurple, logoAlign, logoWrap, right,
		brandShell, brandShell, brandPurple, brandPurple, brandShell, brandShell,
		brandInk,
		body,
		brandShell, brandShell, brandMuted)
}

// --- building blocks the templates share ------------------------------------

func h1(text string) string {
	return fmt.Sprintf(`<h1 style="margin:0 0 18px;font-size:28px;line-height:1.2;font-weight:800;color:%s;">%s</h1>`, brandInk, text)
}

func h1Colour(text, colour string) string {
	return fmt.Sprintf(`<h1 style="margin:0 0 18px;font-size:28px;line-height:1.2;font-weight:800;color:%s;">%s</h1>`, colour, text)
}

// eyebrow is the small line above a title: "Order #BB6VPXNXFF".
func eyebrow(text string) string {
	return fmt.Sprintf(`<p style="margin:0 0 6px;font-size:14px;font-weight:700;color:%s;">%s</p>`, brandInk, text)
}

func para(html string) string {
	return fmt.Sprintf(`<p style="margin:0 0 14px;font-size:14px;line-height:1.7;color:%s;">%s</p>`, brandInk, html)
}

func paraMuted(html string) string {
	return fmt.Sprintf(`<p style="margin:0 0 14px;font-size:13px;line-height:1.7;color:%s;">%s</p>`, brandMuted, html)
}

func strong(text string) string {
	return `<strong style="font-weight:700;">` + template.HTMLEscapeString(text) + `</strong>`
}

// button is the one call to action a card carries.
func button(label, href string) string {
	return fmt.Sprintf(`<table role="presentation" cellpadding="0" cellspacing="0" border="0" align="center" style="margin:26px auto 0;"><tr><td align="center" style="background-color:%s;border-radius:6px;">
  <a href="%s" style="display:inline-block;padding:13px 30px;font-size:14px;font-weight:700;color:#ffffff;text-decoration:none;border-radius:6px;">%s</a>
</td></tr></table>`, brandPurple, template.HTMLEscapeString(href), template.HTMLEscapeString(label))
}

// orderNoBox is the pink panel with the reference in it.
func orderNoBox(reference string) string { return orderNoBoxTitled("Order No", reference) }

// orderNoBoxTitled is orderNoBox with its own heading ("Bill No").
func orderNoBoxTitled(title, value string) string {
	return fmt.Sprintf(`<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0" style="margin:4px 0 22px;"><tr><td align="center" style="background-color:%s;border-radius:8px;padding:18px 16px;">
  <p style="margin:0 0 6px;font-size:12px;letter-spacing:1px;text-transform:uppercase;color:%s;">%s</p>
  <p style="margin:0;font-size:30px;line-height:1.1;font-weight:800;letter-spacing:1px;color:%s;">%s</p>
</td></tr></table>`, brandPink, brandInk, template.HTMLEscapeString(title), brandInk, template.HTMLEscapeString(value))
}

// noticeBox is the soft pink panel a reason or a code sits in.
func noticeBox(html string) string {
	return fmt.Sprintf(`<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0" style="margin:4px 0 18px;"><tr><td style="background-color:#FDF2F8;border:1px solid %s;border-radius:8px;padding:16px 18px;font-size:14px;line-height:1.6;color:%s;">%s</td></tr></table>`, brandPinkLine, brandInk, html)
}

// codeBox is the one-time passcode panel.
func codeBox(code string, validFor string) string {
	return fmt.Sprintf(`<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0" style="margin:4px 0 22px;"><tr><td align="center" style="background-color:%s;border-radius:8px;padding:20px 16px;">
  <p style="margin:0;font-size:34px;line-height:1.1;font-weight:800;letter-spacing:8px;color:%s;font-family:ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,monospace;">%s</p>
  <p style="margin:10px 0 0;font-size:12px;color:%s;">%s</p>
</td></tr></table>`, brandPink, brandInk, template.HTMLEscapeString(code), brandMuted, template.HTMLEscapeString(validFor))
}

// pill is the small rounded label in the header ("2 items").
func pill(text string) string {
	return fmt.Sprintf(`<span style="display:inline-block;background-color:#ffffff;color:%s;font-size:12px;font-weight:700;padding:6px 12px;border-radius:6px;white-space:nowrap;">%s</span>`, brandInk, template.HTMLEscapeString(text))
}

// sectionLabel is the bold "Items" heading above a list.
func sectionLabel(text string) string {
	return fmt.Sprintf(`<p style="margin:22px 0 8px;font-size:14px;font-weight:700;color:%s;">%s</p>`, brandInk, template.HTMLEscapeString(text))
}

// totalsRow is one line of the money summary; bold for the final total.
func totalsRow(label, amount string, total bool) string {
	weight, size, pad := "400", "13px", "5px 0"
	if total {
		weight, size, pad = "800", "14px", "10px 0 0"
	}
	return fmt.Sprintf(`<tr><td style="padding:%s;font-size:%s;font-weight:%s;color:%s;">%s</td><td align="right" style="padding:%s;font-size:%s;font-weight:%s;color:%s;">%s</td></tr>`,
		pad, size, weight, brandInk, template.HTMLEscapeString(label), pad, size, weight, brandInk, template.HTMLEscapeString(amount))
}

// rule is the thin divider between blocks.
func rule() string {
	return fmt.Sprintf(`<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0"><tr><td style="border-top:1px solid %s;font-size:0;line-height:0;height:1px;">&nbsp;</td></tr></table>`, brandLine)
}
