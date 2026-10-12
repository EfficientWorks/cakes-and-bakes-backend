package api

import (
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/httpx"
	"bakehouse/services/payments/internal/domain"
)

type Handler struct {
	svc *domain.Service
}

func NewHandler(svc *domain.Service) *Handler { return &Handler{svc: svc} }

// webhookAck documents the webhook acknowledgement body.
type webhookAck struct {
	Received bool `json:"received" example:"true"`
}

type createCheckoutSessionRequest struct {
	AmountCents int64  `json:"amountCents" example:"4208"`
	Currency    string `json:"currency" example:"GBP"`
	// Email prefills the receipt address on the payment page. Optional.
	Email string `json:"email" example:"tina@example.com"`
	// ReturnPath is a path on the storefront — not a URL. The origin comes
	// from STOREFRONT_BASE_URL on the server, so a caller cannot turn the
	// return leg into an open redirect. Defaults to /checkout.
	ReturnPath string `json:"returnPath" example:"/checkout"`
	// Provider: "stripe" or omitted.
	Provider string `json:"provider,omitempty" example:"stripe"`
}

// checkoutSessionResponse is the Checkout Session body.
type checkoutSessionResponse struct {
	ID  string `json:"id" example:"cs_test_a1B2c3"`
	URL string `json:"url" example:"https://checkout.stripe.com/c/pay/cs_test_a1B2c3"`
	// open | complete | expired
	Status string `json:"status" example:"complete"`
	// unpaid | paid
	PaymentStatus string `json:"paymentStatus" example:"paid"`
	// Empty until the customer has paid. The storefront sends it to
	// POST /storefront/orders as payment.paymentId.
	PaymentID   string `json:"paymentId" example:"pi_3PqRsT"`
	AmountCents int64  `json:"amountCents" example:"4208"`
	Currency    string `json:"currency" example:"GBP"`
}

func checkoutSessionDTO(s *domain.CheckoutSession) checkoutSessionResponse {
	return checkoutSessionResponse{
		ID: s.ID, URL: s.URL, Status: s.Status, PaymentStatus: s.PaymentStatus,
		PaymentID: s.PaymentID, AmountCents: s.AmountCents, Currency: s.Currency,
	}
}

type providersResponse struct {
	// Card providers configured on this server ("stripe"), or empty.
	Providers []string `json:"providers"`
	// Used when a checkout session names no provider.
	Default string `json:"default"`
}

// Providers lists the card providers configured on this server.
//
//	@Summary		Card providers on this server
//	@Description	The card providers with keys on this server. The storefront offers card payment for a store only when its cardProviders also include one of these.
//	@Tags			payments
//	@Produce		json
//	@Success		200	{object}	httpx.Envelope{data=providersResponse}
//	@Router			/storefront/payments/providers [get]
func (h *Handler) Providers(w http.ResponseWriter, r *http.Request) {
	httpx.OK(w, providersResponse{Providers: h.svc.Providers(), Default: h.svc.DefaultProvider()})
}

// CreateCheckoutSession opens a Stripe-hosted payment page.
//
// The storefront calls this when the customer presses Place Order and sends
// the browser to the returned url. No order exists yet: it is created after
// the customer comes back, from the PaymentIntent this session produces, with
// a total recomputed on the server.
//
//	@Summary		Create a Stripe Checkout session
//	@Description	Returns a Stripe-hosted payment page URL to redirect the customer to. The order is created afterwards, from the session's PaymentIntent id, with a server-recomputed total.
//	@Tags			payments
//	@Accept			json
//	@Produce		json
//	@Param			body	body		createCheckoutSessionRequest	true	"Amount in minor units, ISO-4217 currency, and the storefront path to return to"
//	@Success		200		{object}	httpx.Envelope{data=checkoutSessionResponse}
//	@Failure		400		{object}	httpx.Envelope{error=httpx.ErrorBody}	"VALIDATION_FAILED"
//	@Failure		503		{object}	httpx.Envelope{error=httpx.ErrorBody}	"PAYMENTS_NOT_CONFIGURED or STRIPE_ERROR"
//	@Router			/storefront/payments/checkout-session [post]
func (h *Handler) CreateCheckoutSession(w http.ResponseWriter, r *http.Request) {
	var req createCheckoutSessionRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	session, err := h.svc.CreateCheckoutSession(r.Context(), req.AmountCents, req.Currency, req.Email, req.ReturnPath, req.Provider)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, checkoutSessionDTO(session))
}

// GetCheckoutSession reads a session back once Stripe has returned the
// customer, so the storefront can tell a completed payment from an abandoned
// one and pick up the PaymentIntent id.
//
//	@Summary		Read a Stripe Checkout session
//	@Description	Read live from Stripe on every call — the session id arrives in the browser's query string, so its payment status is exactly what must not be taken on trust.
//	@Tags			payments
//	@Produce		json
//	@Param			id	path		string	true	"Checkout session id (cs_…)"
//	@Success		200	{object}	httpx.Envelope{data=checkoutSessionResponse}
//	@Failure		400	{object}	httpx.Envelope{error=httpx.ErrorBody}	"VALIDATION_FAILED"
//	@Failure		503	{object}	httpx.Envelope{error=httpx.ErrorBody}	"PAYMENTS_NOT_CONFIGURED"
//	@Router			/storefront/payments/checkout-session/{id} [get]
func (h *Handler) GetCheckoutSession(w http.ResponseWriter, r *http.Request) {
	session, err := h.svc.GetCheckoutSession(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, checkoutSessionDTO(session))
}

// StripeWebhook receives Stripe events, authenticated by Stripe's own
// signature rather than a customer or staff token.
//
//	@Summary		Stripe webhook
//	@Description	Payment, refund and payment-link updates from Stripe. Verified with the Stripe-Signature header against STRIPE_WEBHOOK_SECRET.
//	@Tags			payments
//	@Accept			json
//	@Produce		json
//	@Param			Stripe-Signature	header		string	true	"Stripe webhook signature"
//	@Success		200					{object}	httpx.Envelope{data=webhookAck}
//	@Failure		401					{object}	httpx.Envelope{error=httpx.ErrorBody}	"INVALID_SIGNATURE"
//	@Failure		503					{object}	httpx.Envelope{error=httpx.ErrorBody}	"WEBHOOK_NOT_CONFIGURED"
//	@Router			/webhooks/stripe [post]
func (h *Handler) StripeWebhook(w http.ResponseWriter, r *http.Request) {
	payload, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		httpx.WriteError(w, r, apperr.Validation("Could not read webhook payload."))
		return
	}
	if err := h.svc.HandleStripeWebhook(r.Context(), payload, r.Header.Get("Stripe-Signature")); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, webhookAck{Received: true})
}
