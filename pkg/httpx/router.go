package httpx

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"bakehouse/pkg/apperr"
)

// NewRouter builds the base chi router with global middleware, a health
// check, and an empty /api/v1 group that module routers mount into.
//
//	r := httpx.NewRouter(log, corsOrigins)
//	r.Route("/api/v1", func(v1 chi.Router) {
//	    identity.Mount(v1, deps)
//	    catalog.Mount(v1, deps)
//	})
func NewRouter(log *slog.Logger, corsOrigins []string) *chi.Mux {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(RequestID)
	r.Use(middleware.Recoverer)
	r.Use(Logger(log))
	r.Use(CORS(corsOrigins))

	r.Get("/health", Health)

	// A path that matches nothing, and a path that matches with the wrong
	// verb, are the two responses a client gets without any handler running.
	// Left to chi they come back as plain text — "404 page not found" — which
	// is the one pair of responses a client parsing our JSON envelope cannot
	// parse. A mistyped URL then fails as a JSON syntax error somewhere far
	// from the mistake.
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		WriteError(w, req, apperr.NotFound("No endpoint at "+req.URL.Path+"."))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, req *http.Request) {
		WriteError(w, req, apperr.New("METHOD_NOT_ALLOWED", apperr.CategoryMethodNotAllowed,
			req.Method+" is not allowed on "+req.URL.Path+"."))
	})

	return r
}
