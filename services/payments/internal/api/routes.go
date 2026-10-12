package api

import "github.com/go-chi/chi/v5"

// MountStorefront attaches the customer-facing payment endpoints onto the
// shared /storefront router.
func MountStorefront(sf chi.Router, h *Handler) {
	sf.Post("/payments/checkout-session", h.CreateCheckoutSession)
	sf.Get("/payments/checkout-session/{id}", h.GetCheckoutSession)
	sf.Get("/payments/providers", h.Providers)
}

// MountWebhook attaches the Stripe webhook directly onto v1 — outside
// /storefront and /admin, since it is neither customer- nor staff-
// authenticated (Stripe's signature is the auth).
func MountWebhook(v1 chi.Router, h *Handler) {
	v1.Post("/webhooks/stripe", h.StripeWebhook)
}
