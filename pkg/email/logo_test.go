package email

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"bakehouse/pkg/tenant"
)

// Without a logo to point at, the header sets the name in type rather than
// shipping an <img> that 404s; with one, it is the business's own, escaped.
func TestHeaderLogoIsTheBusinesses(t *testing.T) {
	if got := logoHTML(Brand{Name: "Cakes & Bakes"}); strings.Contains(got, "<img") || !strings.Contains(got, "Cakes &amp; Bakes") {
		t.Fatalf("no logo should set the name in type: %s", got)
	}
	got := logoHTML(Brand{Name: "C&B", LogoURL: `https://cdn.example.com/logo.png?a=1&b="x`})
	if !strings.Contains(got, `src="https://cdn.example.com/logo.png?a=1&amp;b=&#34;x"`) || !strings.Contains(got, `alt="C&amp;B"`) {
		t.Fatalf("logo URL and name must be escaped: %s", got)
	}
}

// An email with no business goes out as the platform, with EMAIL_LOGO_URL.
func TestPlatformBrandUsesEmailLogoURL(t *testing.T) {
	prevURL, prevExplicit := logoURL, logoURLExplicit
	t.Cleanup(func() { logoURL, logoURLExplicit = prevURL, prevExplicit })
	SetLogoURL("https://cdn.example.com/logo.png")
	b := BrandFor(context.Background(), Email{})
	if b.Name != PlatformBrand.Name || b.LogoURL != "https://cdn.example.com/logo.png" {
		t.Fatalf("platform brand = %+v", b)
	}
}

// A business's email carries its name, its shop and its currency.
func TestBrandForUsesTheRequestsBusiness(t *testing.T) {
	t.Cleanup(func() { SetBrandLookup(nil) })
	SetBrandLookup(func(_ context.Context, id string) (Brand, bool) {
		return Brand{Name: "Biz " + id, Currency: "GBP", WebsiteURL: "https://shop.example"}, id == "b1"
	})
	if got := BrandFor(tenant.WithBusiness(context.Background(), "b1"), Email{}); got.Name != "Biz b1" {
		t.Fatalf("from context: %+v", got)
	}
	if got := BrandFor(context.Background(), Email{BusinessID: "b1"}); got.Name != "Biz b1" {
		t.Fatalf("from email: %+v", got)
	}
	out, err := RenderBranded(BrandFor(context.Background(), Email{BusinessID: "b1"}), TemplateCustomerWelcome, map[string]interface{}{"firstName": "Ana"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Subject, "Biz b1") || !strings.Contains(out.HTMLBody, "https://shop.example/shop") {
		t.Fatalf("welcome email not branded: %s / %s", out.Subject, out.HTMLBody)
	}
}

func TestShellHasSquareBandAndWhiteContainer(t *testing.T) {
	html := shell(shellOptions{Title: "t"}, "body")
	if strings.Contains(html, "border-radius:8px 8px 0 0") {
		t.Error("purple band should have square corners")
	}
	if !strings.Contains(html, "background-color:"+brandShell) {
		t.Error("outer container background missing")
	}
}

func TestLogoHandler(t *testing.T) {
	rec := httptest.NewRecorder()
	LogoHandler(rec, httptest.NewRequest(http.MethodGet, LogoPath, nil))
	want := http.StatusNotFound
	if HasEmbeddedLogo() {
		want = http.StatusOK
	}
	if rec.Code != want {
		t.Fatalf("status = %d, want %d", rec.Code, want)
	}
}
