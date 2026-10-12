package email

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Without a logo to point at, the header sets the name in type rather than
// shipping an <img> that 404s — the broken-image icon every email carried
// while the logo URL defaulted onto a storefront that does not host it.
func TestHeaderLogoFallsBackToTextWithoutALogo(t *testing.T) {
	prevURL, prevExplicit := logoURL, logoURLExplicit
	t.Cleanup(func() { logoURL, logoURLExplicit = prevURL, prevExplicit })

	logoURL, logoURLExplicit = "", false
	SetAssetBaseURL("https://api.example.com")
	if HasEmbeddedLogo() {
		if want := "https://api.example.com" + LogoPath; logoURL != want {
			t.Fatalf("logoURL = %q, want %q", logoURL, want)
		}
	} else if strings.Contains(headerLogo(), "<img") {
		t.Fatalf("header still renders an <img> with no logo available: %s", headerLogo())
	}

	SetLogoURL("https://cdn.example.com/logo.png")
	SetAssetBaseURL("https://api.example.com")
	if !strings.Contains(headerLogo(), `src="https://cdn.example.com/logo.png"`) {
		t.Fatalf("EMAIL_LOGO_URL should win over the served default: %s", headerLogo())
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
