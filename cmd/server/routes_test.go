package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/auth"
	"bakehouse/pkg/media"
	"bakehouse/services/admin"
	"bakehouse/services/business"
	"bakehouse/services/catalog"
	"bakehouse/services/enquiries"
	"bakehouse/services/franchise"
	"bakehouse/services/identity"
	"bakehouse/services/orders"
	"bakehouse/services/payments"
)

// newTestRouter assembles every module against a nil pool. Mounting routes
// never touches the database — only serving a request would — so this
// exercises the whole route tree without Postgres.
func newTestRouter(t *testing.T) *chi.Mux {
	t.Helper()

	customerTokens := auth.NewIssuer("test-customer-secret", "test")
	staffTokens := auth.NewIssuer("test-staff-secret", "test")

	franchiseSvc := franchise.New(nil, nil, nil)
	catalogSvc := catalog.New(nil, nil, franchiseSvc.Branches(), media.DisabledStorer{})
	paymentsSvc := payments.New(nil, payments.Config{StorefrontBaseURL: "http://localhost:3000"})

	return buildRouter(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		true,
		[]string{"http://localhost:3000"},
		customerTokens, staffTokens,
		services{
			identity:  identity.New(nil, customerTokens, staffTokens, nil, nil, identity.TokenTTLs{}),
			franchise: franchiseSvc,
			catalog:   catalogSvc,
			orders: orders.New(nil, orders.Deps{
				Branches: franchiseSvc.Branches(),
				Pricing:  catalogSvc.Pricing(),
				Payments: paymentsSvc.Processor(),
			}),
			payments:  paymentsSvc,
			enquiries: enquiries.New(nil, franchiseSvc.Branches(), nil, nil),
			business:  business.New(nil, nil, "test-platform-key"),
			admin:     admin.New(nil),
		},
	)
}

// TestRouterMounts is the regression test for the shared-prefix wiring.
// Four services attach routes to /admin/branches and two to
// /admin/franchises; chi panics at registration time if any of them
// double-mounts a prefix or registers a duplicate pattern. That panic would
// otherwise only surface on boot.
func TestRouterMounts(t *testing.T) {
	r := newTestRouter(t) // panics here on a bad mount

	var routes []string
	err := chi.Walk(r, func(method string, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routes = append(routes, method+" "+route)
		return nil
	})
	if err != nil {
		t.Fatalf("walk routes: %v", err)
	}
	if len(routes) == 0 {
		t.Fatal("no routes registered")
	}

	// Every service must have contributed. If a Mount call is dropped
	// during a refactor the tree still builds, so assert on the prefixes.
	wantPrefixes := []string{
		"/v1/auth/",                      // identity, public
		"/v1/staff/auth/",                // identity, staff
		"/v1/account/",                   // identity + orders
		"/v1/storefront/",                // franchise + catalog + orders + payments
		"/v1/admin/staff",                // identity, admin
		"/v1/admin/governance",           // franchise
		"/v1/admin/products",             // catalog
		"/v1/admin/orders",               // orders
		"/v1/admin/branches/",            // shared by four services
		"/v1/admin/network/",             // admin
		"/v1/admin/customers",            // admin
		"/v1/orders/track/",              // orders, public tracking
		"/v1/webhooks/stripe",            // payments
		"/v1/admin/integrations",         // settings page
		"/v1/enquiries/",                 // enquiries, public forms
		"/v1/platform/businesses",        // business, platform API
		"/v1/newsletter/",                // enquiries, newsletter
		"/v1/admin/enquiries",            // enquiries, admin queue
		"/v1/storefront/product-filters", // catalog, Shop Now facets
	}
	joined := strings.Join(routes, "\n")
	for _, prefix := range wantPrefixes {
		if !strings.Contains(joined, prefix) {
			sort.Strings(routes)
			t.Errorf("no route registered under %s\nregistered:\n%s", prefix, joined)
		}
	}
}

// TestHealthAndDocs checks the two routes that sit outside /v1.
func TestHealthAndDocs(t *testing.T) {
	r := newTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET /health = %d, want 200", rec.Code)
	}

	// Swagger UI is mounted outside production.
	req = httptest.NewRequest(http.MethodGet, "/docs", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusMovedPermanently {
		t.Errorf("GET /docs = %d, want 301 redirect to /docs/index.html", rec.Code)
	}
}

// TestAuthRequired confirms the two authenticated groups reject an
// anonymous request rather than reaching a handler (and a nil database).
func TestAuthRequired(t *testing.T) {
	r := newTestRouter(t)

	for _, path := range []string{"/v1/account/profile", "/v1/admin/orders"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s without a token = %d, want 401", path, rec.Code)
		}
	}
}
