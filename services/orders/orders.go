// Package orders is the composition root for the orders service: cart,
// branch routing, slot scheduling, checkout, amendment, cancellation and
// refund.
//
// Scheduling lives here as an internal concern rather than as a service of
// its own (ADR-0006): reserving slot capacity and creating the order have
// to happen in one transaction, so splitting them would put a transaction
// boundary in exactly the wrong place.
//
// Everything under internal/ is private to this service; the types it
// publishes live in ordersapi.
package orders

import (
	"bakehouse/pkg/geo"
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"bakehouse/pkg/audit"
	"bakehouse/pkg/email"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/franchise/franchiseapi"
	"bakehouse/services/orders/internal/api"
	"bakehouse/services/orders/internal/domain"
	"bakehouse/services/orders/internal/store"
	"bakehouse/services/payments/paymentsapi"
)

// Deps are the other services orders reads through. Each is a narrow
// interface owned by the service that publishes it, so orders never holds a
// reference to another service's implementation.
type Deps struct {
	// Branches resolves branch config: delivery zone, hours, fees, timezone.
	Branches franchiseapi.BranchReader
	// Pricing re-prices every line server-side at add-to-cart and again at
	// checkout.
	Pricing catalogapi.Pricer
	// Coupons prices discount codes and records their redemption. Nil leaves
	// the cart's discount at zero and rejects every code, which is what a
	// partial wiring should do rather than silently giving money away.
	Coupons catalogapi.CouponPricer
	// Sales feeds the catalogue's popularity ranking from real order volume,
	// once an order is marked fulfilled. Nil disables the rollup: orders are
	// still fulfillable, the ranking just stops moving.
	Sales catalogapi.SalesRecorder
	// Payments captures and refunds money. Nil falls back to a no-op
	// processor, which keeps orders placeable without Stripe configured.
	Payments paymentsapi.Processor
	// Emailer sends order confirmations.
	Emailer email.Emailer
	// Audit records every staff mutation.
	Audit *audit.Writer
	// LocationIQAccessToken powers the address finder.
	LocationIQAccessToken string
}

// Module is the assembled orders service.
type Module struct {
	store   *store.Store
	svc     *domain.Service
	handler *api.Handler
}

// New assembles the service from its store and the seams in deps.
func New(pool *pgxpool.Pool, deps Deps) *Module {
	st := store.NewStore(pool)
	svc := domain.NewService(st, deps.Branches, deps.Pricing, deps.Coupons, deps.Sales, deps.Payments, deps.Emailer, deps.Audit, deps.LocationIQAccessToken)
	return &Module{store: st, svc: svc, handler: api.NewHandler(svc)}
}

// UseKDS wires the franchise lookups the Kitchen Display needs.
func (m *Module) UseKDS(r franchiseapi.KDSReader) { m.svc.UseKDS(r) }

// UseTagRestrictions wires seasonal tags' restricted dates (website orders).
func (m *Module) UseTagRestrictions(r catalogapi.TagRestrictions) { m.svc.UseTagRestrictions(r) }

// UseCardPayments wires Stripe payment links and card refunds.
func (m *Module) UseCardPayments(c paymentsapi.Cards) { m.svc.UseCardPayments(c) }

// StartScheduledPromoter moves scheduled orders to today on their day, in
// the background until ctx ends.
func (m *Module) StartScheduledPromoter(ctx context.Context) { m.svc.StartScheduledPromoter(ctx) }

// HandlePaymentEvent receives provider webhook events (link paid or expired,
// refund settled) from the payments module.
func (m *Module) HandlePaymentEvent(ctx context.Context, e paymentsapi.Event) {
	m.svc.HandlePaymentEvent(ctx, e)
}

// MountKDS attaches the KDS order endpoints under /kds.
func (m *Module) MountKDS(r chi.Router) { api.MountKDS(r, m.handler) }

// KDSEventsHandler serves the KDS live stream; mounted with query-token auth.
func (m *Module) KDSEventsHandler() http.HandlerFunc { return m.handler.KDSEvents }

// MountStorefront attaches cart, slot lookup, checkout validation and order
// creation. Guest checkout is supported, so the router this mounts onto
// carries OptionalCustomer rather than RequireCustomer.
func (m *Module) MountStorefront(r chi.Router) { api.MountStorefront(r, m.handler) }

// MountPublicTracking attaches the unauthenticated order-tracking endpoint.
func (m *Module) MountPublicTracking(r chi.Router) { api.MountPublicTracking(r, m.handler) }

// MountAccount attaches order history and customer self-service onto the
// shared /account router.
func (m *Module) MountAccount(r chi.Router) { api.MountAccount(r, m.handler) }

// MountPOS attaches the till's own endpoints under /pos.
func (m *Module) MountPOS(r chi.Router) { api.MountPOS(r, m.handler) }

// MountAdminOrders attaches staff order management onto the shared /admin
// router.
func (m *Module) MountAdminOrders(r chi.Router) { api.MountAdminOrders(r, m.handler) }

// MountAdminBranches attaches schedule and slot configuration onto the
// shared /admin/branches router.
func (m *Module) MountAdminBranches(r chi.Router) { api.MountAdminBranches(r, m.handler) }

// UseAddressGeocoder wires street-address geocoding for radius delivery
// pricing at the cart, checkout and till.
func (m *Module) UseAddressGeocoder(g geo.AddressGeocoder) { m.svc.UseAddressGeocoder(g) }
