// Package franchise is the composition root for the franchise service:
// the Brand -> Franchise -> Branch hierarchy, governance rules, delivery
// zones and branch exceptions (ADR-0002).
//
// Everything under internal/ is private to this service — the Go compiler
// enforces that, which is what replaces the process boundary a separate
// deployment would have given us. Other services depend on this one only
// through franchiseapi, never by reaching into internal/.
package franchise

import (
	"context"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"bakehouse/pkg/audit"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/geo"
	"bakehouse/services/franchise/franchiseapi"
	"bakehouse/services/franchise/internal/api"
	"bakehouse/services/franchise/internal/domain"
	"bakehouse/services/franchise/internal/store"
)

// Module is the assembled franchise service.
type Module struct {
	store   *store.Store
	svc     *domain.Service
	handler *api.Handler
}

// New assembles the service from its dependencies. Call once at startup.
func New(pool *pgxpool.Pool, auditWriter *audit.Writer, geocoder geo.Geocoder) *Module {
	st := store.NewStore(pool)
	svc := domain.NewService(st, auditWriter, geocoder)
	return &Module{store: st, svc: svc, handler: api.NewHandler(svc)}
}

// Branches exposes the branch lookups other services depend on. This is the
// only supported way to read franchise data from outside this package.
func (m *Module) Branches() franchiseapi.BranchReader { return m.store }

// BusinessIDResolver exposes the Business ID lookup the POS login needs.
//
// Narrower than Branches(): identity wants one code turned into two ids, and
// giving it the whole branch reader would invite more coupling than that.
func (m *Module) BusinessIDResolver() *store.Store { return m.store }

// ScopeStatus exposes the "is this business still trading" lookup.
func (m *Module) ScopeStatus() *store.Store { return m.store }

// KDS exposes central-kitchen coverage and KDS settings to the orders service.
func (m *Module) KDS() franchiseapi.KDSReader { return m.store }

// MountKDS attaches KDS settings and printers under /kds.
func (m *Module) MountKDS(r chi.Router) { api.MountKDS(r, m.handler) }

// MountStorefront attaches the public delivery-eligibility endpoints.
func (m *Module) MountStorefront(r chi.Router) { api.MountStorefront(r, m.handler) }

// MountPOS attaches printer configuration to the till.
func (m *Module) MountPOS(r chi.Router) { api.MountPOS(r, m.handler) }

// MountAdmin attaches the governance-rules endpoint.
func (m *Module) MountAdmin(r chi.Router) { api.MountAdmin(r, m.handler) }

// MountAdminBranches attaches branch CRUD onto the shared /admin/branches
// router.
func (m *Module) MountAdminBranches(r chi.Router) { api.MountAdminBranches(r, m.handler) }

// MountAdminBranchRequests attaches branch-request lifecycle endpoints onto
// the shared /admin/branch-requests router.
func (m *Module) MountAdminBranchRequests(r chi.Router) {
	api.MountAdminBranchRequests(r, m.handler)
}

// --- Bootstrap seam ---------------------------------------------------------
//
// The four methods below exist so cmd/seed can create the first franchise
// and branch on a fresh database without reaching into internal/. They are
// the same authorised paths the HTTP handlers use — claims are still
// re-checked against the governance matrix on every call, so seeding cannot
// create something a super admin could not create through the API.

// ListBranches returns the branches visible to claims.
func (m *Module) ListBranches(ctx context.Context, claims *auth.StaffClaims) ([]franchiseapi.Branch, error) {
	return m.svc.ListBranches(ctx, claims, "")
}

// CreateBranch creates a branch, returning any non-fatal warnings (for
// example a delivery zone that overlaps an existing branch).
func (m *Module) CreateBranch(ctx context.Context, claims *auth.StaffClaims, b franchiseapi.Branch) (*franchiseapi.Branch, []string, error) {
	return m.svc.CreateBranch(ctx, claims, b)
}

// UseAddressGeocoder wires street-address geocoding: stores are located from
// their address when saved (radius delivery measures from there), and
// customers when delivery is checked.
func (m *Module) UseAddressGeocoder(g geo.AddressGeocoder) { m.svc.UseAddressGeocoder(g) }
