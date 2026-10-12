// Package business is the composition root for the platform API: the emzo
// team registers a business (a bakery chain), sets its country — which
// decides its currency, timezone and tax — and gives it an owner login. The
// owner then builds their stores, staff and menu in the admin console.
//
// Owner logins are created through identity's published BusinessOwners seam;
// this service never touches staff tables itself.
package business

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"bakehouse/services/business/internal/api"
	"bakehouse/services/business/internal/domain"
	"bakehouse/services/business/internal/store"
	"bakehouse/services/identity/identityapi"
)

// Module is the assembled business service.
type Module struct {
	handler     *api.Handler
	store       *store.Store
	platformKey string
}

// New assembles the service. platformKey is PLATFORM_API_KEY; empty keeps
// the platform API closed (every call answers 503).
func New(pool *pgxpool.Pool, owners identityapi.BusinessOwners, platformKey string) *Module {
	st := store.NewStore(pool)
	svc := domain.NewService(st, owners)
	return &Module{handler: api.NewHandler(svc), store: st, platformKey: platformKey}
}

// MountPlatform attaches /platform/* onto v1.
func (m *Module) MountPlatform(v1 chi.Router) { api.MountPlatform(v1, m.handler, m.platformKey) }

// MountAdmin attaches GET /admin/business onto the staff /admin router.
func (m *Module) MountAdmin(admin chi.Router) { api.MountAdmin(admin, m.handler) }

// ResolveBusiness is the middleware that sets the business a public request
// is for (X-Business header, else defaultSlug). See api.ResolveBusiness.
func (m *Module) ResolveBusiness(defaultSlug string) func(http.Handler) http.Handler {
	return api.ResolveBusiness(m.store.GetBusinessBySlug, defaultSlug)
}
