package email

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"time"
)

// LogoPath is where the API serves the email wordmark.
const LogoPath = "/email/logo-white.png"

// The email assets ship inside the binary: the runtime image carries only the
// server and the migrations, so a file on disk would never reach production.
//
//go:embed assets
var assets embed.FS

func logoBytes() []byte {
	b, err := fs.ReadFile(assets, "assets/logo-white.png")
	if err != nil {
		return nil
	}
	return b
}

// HasEmbeddedLogo reports whether assets/logo-white.png was compiled in.
func HasEmbeddedLogo() bool { return len(logoBytes()) > 0 }

// LogoHandler serves the embedded wordmark. Mail clients and image proxies
// (Gmail's among them) fetch it anonymously, so it sits outside auth, and it
// is cached for a day — it changes only with a deploy.
func LogoHandler(w http.ResponseWriter, r *http.Request) {
	b := logoBytes()
	if b == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	http.ServeContent(w, r, "logo-white.png", time.Time{}, bytes.NewReader(b))
}
