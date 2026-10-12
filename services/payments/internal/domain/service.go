package domain

import (
	"context"
	"log/slog"
	"strings"

	"github.com/stripe/stripe-go/v83"

	"bakehouse/pkg/apperr"
	"bakehouse/services/payments/internal/store"
	"bakehouse/services/payments/paymentsapi"
)

// Service owns the payments ledger and every call to Stripe: the storefront's
// hosted checkout, POS payment links, refunds and the webhook.
type Service struct {
	store *store.Store
	// The origin the storefront is served on. Hosted payment pages send the
	// customer back to a URL built from this, never from the request: a
	// caller-chosen return URL would be an open redirect.
	storefrontBaseURL string

	// Nil until UseStripe: every Stripe-backed call then answers
	// PROVIDER_NOT_CONFIGURED instead of panicking.
	stripe              *stripe.Client
	stripeWebhookSecret string
	refunds             paymentsapi.Processor

	onEvent paymentsapi.EventHandler
}

// checkoutLabel is the single line item a customer sees on the hosted page.
// The cart is not itemised there on purpose: lines are re-priced on the
// server when the order is created, and a page listing items the re-price
// then disagrees with would be worse than one honest total.
const checkoutLabel = "Your order"

// statementDescription appears on the Stripe dashboard and the receipt.
const statementDescription = "Online order"

func NewService(st *store.Store, storefrontBaseURL string) *Service {
	return &Service{store: st, storefrontBaseURL: strings.TrimRight(storefrontBaseURL, "/")}
}

// UseStripe connects the service to Stripe. refunds is the processor refunds
// are made through.
func (s *Service) UseStripe(client *stripe.Client, webhookSecret string, refunds paymentsapi.Processor) {
	s.stripe = client
	s.stripeWebhookSecret = webhookSecret
	s.refunds = refunds
}

// OnEvent sets who hears about webhook events (orders).
func (s *Service) OnEvent(h paymentsapi.EventHandler) { s.onEvent = h }

func (s *Service) emit(ctx context.Context, e paymentsapi.Event) {
	if s.onEvent == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			slog.Error("payment event handler panicked", "event", e.Type, "order", e.OrderID, "panic", r)
		}
	}()
	s.onEvent(context.WithoutCancel(ctx), e)
}

// Providers are the card providers configured on this server.
func (s *Service) Providers() []string {
	if s.stripe == nil {
		return []string{}
	}
	return []string{paymentsapi.ProviderStripe}
}

// DefaultProvider is where a checkout session goes when none is named.
func (s *Service) DefaultProvider() string {
	if s.stripe == nil {
		return ""
	}
	return paymentsapi.ProviderStripe
}

// requireStripe answers PROVIDER_NOT_CONFIGURED when no Stripe key is set, or
// when the caller named a provider other than Stripe.
func (s *Service) requireStripe(provider string) error {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider != "" && provider != paymentsapi.ProviderStripe {
		return apperr.Validation("provider must be stripe.")
	}
	if s.stripe == nil {
		return errProviderNotConfigured()
	}
	return nil
}

func errProviderNotConfigured() error {
	return apperr.New("PROVIDER_NOT_CONFIGURED", apperr.CategoryBusinessRule,
		"Card payments are not set up on this server.")
}
