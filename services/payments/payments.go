// Package payments is the composition root for the payments service: Stripe
// PaymentIntents and refunds, Stripe Checkout (the storefront's hosted payment
// page and the POS payment links), and the Stripe webhook.
//
// Everything under internal/ is private to this service; other services
// depend on it only through paymentsapi — orders, in particular, knows
// nothing about Stripe and talks only to paymentsapi.Processor and
// paymentsapi.Cards.
package payments

import (
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stripe/stripe-go/v83"

	"bakehouse/services/payments/internal/api"
	"bakehouse/services/payments/internal/domain"
	"bakehouse/services/payments/internal/store"
	"bakehouse/services/payments/paymentsapi"
)

// Module is the assembled payments service.
type Module struct {
	svc        *domain.Service
	handler    *api.Handler
	processor  paymentsapi.Processor
	configured bool
}

// Config is what the payments module needs from the environment.
type Config struct {
	StripeSecretKey     string
	StripeWebhookSecret string
	// StorefrontBaseURL is the only origin a hosted payment page sends the
	// customer back to; it is never taken from a request.
	StorefrontBaseURL string
}

// New assembles the service. Without a Stripe key the module still works:
// Processor is a NoopProcessor, so orders can be placed end to end with real
// slot reservations and only the money movement stubbed. Check Configured to
// warn at startup.
func New(pool *pgxpool.Pool, cfg Config) *Module {
	st := store.NewStore(pool)
	m := &Module{
		svc:       domain.NewService(st, cfg.StorefrontBaseURL),
		processor: paymentsapi.NoopProcessor{},
	}
	if cfg.StripeSecretKey != "" {
		client := stripe.NewClient(cfg.StripeSecretKey)
		proc := domain.NewStripeProcessor(client, st)
		m.svc.UseStripe(client, cfg.StripeWebhookSecret, proc)
		m.processor = proc
		m.configured = true
	}
	m.handler = api.NewHandler(m.svc)
	return m
}

// Configured reports whether a Stripe secret key was supplied.
func (m *Module) Configured() bool { return m.configured }

// Processor is the seam orders captures and refunds through.
func (m *Module) Processor() paymentsapi.Processor { return m.processor }

// Cards is the seam orders uses for payment links and provider refunds.
func (m *Module) Cards() paymentsapi.Cards { return m.svc }

// OnEvent reports provider webhook events (link paid or expired, refund
// settled) to h — orders, which records them and tells the screens.
func (m *Module) OnEvent(h paymentsapi.EventHandler) { m.svc.OnEvent(h) }

// MountStorefront attaches the hosted-checkout endpoints.
func (m *Module) MountStorefront(r chi.Router) { api.MountStorefront(r, m.handler) }

// MountWebhook attaches the Stripe webhook, which is authenticated by
// Stripe's own signature rather than by a customer or staff token.
func (m *Module) MountWebhook(r chi.Router) { api.MountWebhook(r, m.handler) }
