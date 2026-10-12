package httpx

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	httpSwagger "github.com/swaggo/http-swagger/v2"
)

// MountSwagger serves the generated OpenAPI spec and Swagger UI at
// /docs/index.html, with the raw spec at /docs/doc.json.
//
// The spec is generated from the // @... annotations on the handlers by
// `make swagger`, and the generated package is imported for its side effect
// in cmd/server. Conventions require every shipped endpoint to carry an
// annotation, so this UI is the API reference rather than a nicety.
//
// Whether it is served at all is the caller's decision, not this function's:
// docs default off in production, where the spec would enumerate every admin
// route and its shape, and on everywhere else. An operator can override either
// way with DOCS_ENABLED — see config.DocsEnabled.
func MountSwagger(r chi.Router, enabled bool) {
	if !enabled {
		return
	}
	r.Get("/docs/*", httpSwagger.Handler(
		httpSwagger.URL("/docs/doc.json"),
		httpSwagger.DocExpansion("none"),
	))
	r.Get("/docs", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "/docs/index.html", http.StatusMovedPermanently)
	})
}
