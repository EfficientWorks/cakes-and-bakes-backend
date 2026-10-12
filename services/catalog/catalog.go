// Package catalog is the composition root for the catalogue service: the
// master catalogue, the 4-layer availability cascade, and line-item
// pricing.
//
// Everything under internal/ is private to this service; other services
// depend on it only through catalogapi.
package catalog

import (
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"bakehouse/pkg/audit"
	"bakehouse/pkg/media"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/catalog/internal/api"
	"bakehouse/services/catalog/internal/domain"
	"bakehouse/services/catalog/internal/store"
	"bakehouse/services/franchise/franchiseapi"
)

// Module is the assembled catalogue service.
type Module struct {
	store   *store.Store
	svc     *domain.Service
	handler *api.Handler
}

// New assembles the service. branches is the franchise service's read
// surface — the availability cascade needs branch configuration and
// timezone to resolve layer 2 and 3.
func New(pool *pgxpool.Pool, auditWriter *audit.Writer, branches franchiseapi.BranchReader, storer media.Storer) *Module {
	st := store.NewStore(pool)
	svc := domain.NewService(st, auditWriter, branches)
	return &Module{store: st, svc: svc, handler: api.NewHandler(svc, storer)}
}

// UseVideos turns on product video upload and delivery.
func (m *Module) UseVideos(v media.VideoStorer) { m.handler.UseVideos(v) }

// Pricing exposes line-item pricing to the orders service. Orders re-prices
// every line through this seam at checkout and never trusts a client price.
func (m *Module) Pricing() catalogapi.Pricer { return m.svc }

// Sales exposes the popularity rollup to the orders service, which calls it
// when an order is marked fulfilled. This is what makes "Most Gifted" — and
// the home page's best-sellers rail — reflect real order volume rather than
// sitting at the zero every product ships with.
func (m *Module) Sales() catalogapi.SalesRecorder { return m.svc }

// Coupons is the discount-pricing seam orders reads through.
func (m *Module) Coupons() catalogapi.CouponPricer { return m.svc }

// TagRestrictions is the seasonal tags' restricted dates, for orders.
func (m *Module) TagRestrictions() catalogapi.TagRestrictions { return m.svc }

// MountStorefront attaches the public catalogue endpoints.
func (m *Module) MountStorefront(r chi.Router) { api.MountStorefront(r, m.handler) }

// MountPublicMedia attaches image serving for locally-stored images.
func (m *Module) MountPublicMedia(r chi.Router) { api.MountPublicMedia(r, m.handler) }

// MountPOS attaches the till's catalogue reads.
func (m *Module) MountPOS(r chi.Router) { api.MountPOS(r, m.handler) }

// MountAdmin attaches master-catalogue CRUD and site content.
func (m *Module) MountAdmin(r chi.Router) { api.MountAdmin(r, m.handler) }

// MountAdminBranchAvailability attaches branch-scoped availability routes
// onto the shared /admin/branches router.
func (m *Module) MountAdminBranchAvailability(r chi.Router) {
	api.MountAdminBranchAvailability(r, m.handler)
}
