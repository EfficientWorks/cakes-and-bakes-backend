// Package business is the composition root for the platform API: the emzo
// team registers a business (a bakery chain), sets its country — which
// decides its currency, timezone and tax — and gives it an owner login. The
// owner then builds their stores, staff and menu in the admin console.
//
// Owner logins are created through identity's published BusinessOwners seam;
// this service never touches staff tables itself.
package business

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"bakehouse/pkg/media"
	"bakehouse/services/business/businessapi"
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

	cacheMu sync.Mutex
	cache   map[string]cachedBusiness
}

// New assembles the service. platformKey is PLATFORM_API_KEY; empty keeps
// the platform API closed (every call answers 503).
func New(pool *pgxpool.Pool, owners identityapi.BusinessOwners, platformKey string) *Module {
	st := store.NewStore(pool)
	svc := domain.NewService(st, owners)
	return &Module{handler: api.NewHandler(svc), store: st, platformKey: platformKey, cache: map[string]cachedBusiness{}}
}

// UseMedia sets where uploaded logos are stored (the product image store).
func (m *Module) UseMedia(store media.Storer) { m.handler.UseMedia(store) }

// MountPlatform attaches /platform/* onto v1.
func (m *Module) MountPlatform(v1 chi.Router) { api.MountPlatform(v1, m.handler, m.platformKey) }

// MountAdmin attaches GET /admin/business onto the staff /admin router.
func (m *Module) MountAdmin(admin chi.Router) { api.MountAdmin(admin, m.handler) }

// MountStorefront attaches GET /storefront/business onto the /storefront
// router.
func (m *Module) MountStorefront(sf chi.Router) { api.MountStorefront(sf, m.handler) }

// ResolveBusiness is the middleware that sets the business a public request
// is for. See api.ResolveBusiness.
func (m *Module) ResolveBusiness(defaultSlug string) func(http.Handler) http.Handler {
	return api.ResolveBusiness(m.lookup(m.store.GetBusinessBySlug), m.lookup(m.store.GetBusinessByWebsite), defaultSlug)
}

// lookup adapts a store read to the resolver: not found is (nil, nil).
func (m *Module) lookup(read func(context.Context, string) (*businessapi.Business, error)) func(context.Context, string) (*businessapi.Business, error) {
	return func(ctx context.Context, key string) (*businessapi.Business, error) {
		b, err := read(ctx, key)
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil
		}
		return b, err
	}
}

// Business reads one business, for other services: the brand on its emails
// and the website its payment pages return to. Cached for five minutes —
// it is read on every email and payment, and changes rarely.
func (m *Module) Business(ctx context.Context, id string) (*businessapi.Business, error) {
	m.cacheMu.Lock()
	e, ok := m.cache[id]
	m.cacheMu.Unlock()
	if ok && time.Since(e.at) < businessByIDTTL {
		return e.business, nil
	}
	b, err := m.store.GetBusiness(ctx, id)
	if err != nil {
		return nil, err
	}
	m.cacheMu.Lock()
	if len(m.cache) >= businessByIDMax {
		m.cache = map[string]cachedBusiness{}
	}
	m.cache[id] = cachedBusiness{business: b, at: time.Now()}
	m.cacheMu.Unlock()
	return b, nil
}

const (
	businessByIDTTL = 5 * time.Minute
	businessByIDMax = 4096
)

type cachedBusiness struct {
	business *businessapi.Business
	at       time.Time
}
