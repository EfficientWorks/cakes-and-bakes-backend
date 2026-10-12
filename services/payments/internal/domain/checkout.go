package domain

import (
	"context"
	"strings"

	"github.com/stripe/stripe-go/v83"

	"bakehouse/pkg/apperr"
)

/*
 * Storefront checkout on a Stripe-hosted payment page.
 *
 * The order is *not* created here. Checkout runs in this order:
 *
 *  1. the storefront asks for a session and sends the browser to session.URL
 *  2. the customer pays on Stripe's hosted page (3-D Secure runs there)
 *  3. Stripe sends them back with ?session_id={CHECKOUT_SESSION_ID}
 *  4. the storefront reads the session back (GetCheckoutSession), takes the
 *     PaymentIntent id, and calls POST /storefront/orders with it
 *  5. order creation runs VerifyIntent, which re-reads the intent from Stripe
 *     and rejects it unless it succeeded *and* its amount matches the total
 *     the server has just recomputed
 *
 * So the amount here is only what the customer is asked to pay. It never
 * becomes the order total, which is why this endpoint can take an amount from
 * the client at all.
 */

// CheckoutSession is what the storefront needs to know about a hosted
// payment page: where to send the browser and, once the customer is back,
// whether the money moved and which payment proves it.
type CheckoutSession struct {
	ID            string
	URL           string
	Status        string // open | complete | expired
	PaymentStatus string // unpaid | paid
	PaymentID     string // Stripe PaymentIntent id, once paid
	AmountCents   int64
	Currency      string
}

// CreateCheckoutSession opens a Stripe Checkout page for amountCents.
// returnPath is a path on the storefront, never a URL.
func (s *Service) CreateCheckoutSession(ctx context.Context, amountCents int64, currency, customerEmail, returnPath, provider string) (*CheckoutSession, error) {
	if err := s.requireCheckout(provider); err != nil {
		return nil, err
	}
	if amountCents <= 0 {
		return nil, apperr.Validation("amountCents must be greater than zero.")
	}
	if currency == "" {
		return nil, apperr.Validation("currency is required.")
	}

	base := s.shopBase(ctx) + safeReturnPath(returnPath)
	joiner := "?"
	if strings.Contains(base, "?") {
		joiner = "&"
	}
	params := &stripe.CheckoutSessionCreateParams{
		Mode: stripe.String(string(stripe.CheckoutSessionModePayment)),
		LineItems: []*stripe.CheckoutSessionCreateLineItemParams{{
			Quantity: stripe.Int64(1),
			PriceData: &stripe.CheckoutSessionCreateLineItemPriceDataParams{
				Currency:   stripe.String(strings.ToLower(currency)),
				UnitAmount: stripe.Int64(amountCents),
				ProductData: &stripe.CheckoutSessionCreateLineItemPriceDataProductDataParams{
					Name: stripe.String(checkoutLabel),
				},
			},
		}},
		// {CHECKOUT_SESSION_ID} is filled in by Stripe, not by us.
		SuccessURL: stripe.String(base + joiner + "session_id={CHECKOUT_SESSION_ID}"),
		CancelURL:  stripe.String(base + joiner + "checkout=cancelled"),
		PaymentIntentData: &stripe.CheckoutSessionCreatePaymentIntentDataParams{
			Description: stripe.String(statementDescription),
		},
	}
	if customerEmail != "" {
		params.CustomerEmail = stripe.String(customerEmail)
	}

	cs, err := s.stripe.V1CheckoutSessions.Create(ctx, params)
	if err != nil {
		return nil, apperr.Wrap(err, "STRIPE_ERROR", apperr.CategoryUpstream, "Could not open the payment page. Please try again.")
	}
	return &CheckoutSession{
		ID: cs.ID, URL: cs.URL, Status: "open", PaymentStatus: "unpaid",
		AmountCents: amountCents, Currency: strings.ToUpper(currency),
	}, nil
}

// GetCheckoutSession reads a session from Stripe every time: its id arrives
// in the browser's query string, so its payment status is exactly what must
// not be taken on trust.
func (s *Service) GetCheckoutSession(ctx context.Context, id string) (*CheckoutSession, error) {
	if err := s.requireCheckout(""); err != nil {
		return nil, err
	}
	if !isStripeSessionID(id) {
		return nil, apperr.Validation("A checkout session id is required.")
	}
	cs, err := s.stripe.V1CheckoutSessions.Retrieve(ctx, id, nil)
	if err != nil {
		return nil, apperr.Wrap(err, "NOT_FOUND", apperr.CategoryUpstream, "That payment session could not be found.")
	}
	out := &CheckoutSession{
		ID: cs.ID, URL: cs.URL, Status: string(cs.Status), PaymentStatus: "unpaid",
		AmountCents: cs.AmountTotal, Currency: strings.ToUpper(string(cs.Currency)),
	}
	if cs.PaymentStatus == stripe.CheckoutSessionPaymentStatusPaid && cs.PaymentIntent != nil {
		out.PaymentStatus = "paid"
		out.PaymentID = cs.PaymentIntent.ID
	}
	return out, nil
}

// requireCheckout is requireStripe with the storefront's own error code.
func (s *Service) requireCheckout(provider string) error {
	if s.stripe == nil {
		return apperr.New("PAYMENTS_NOT_CONFIGURED", apperr.CategoryUpstream, "Card payments are not configured on this environment yet.")
	}
	return s.requireStripe(provider)
}

// isStripeSessionID guards the Stripe call: anything else is not a session
// this server opened.
func isStripeSessionID(id string) bool { return strings.HasPrefix(id, "cs_") }

// safeReturnPath keeps a path, and only a path. Anything that could be read
// as an origin ("//evil.test", "https://evil.test") is discarded rather than
// cleaned, because a cleaner that is wrong once is an open redirect.
func safeReturnPath(p string) string {
	if p == "" || !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.Contains(p, "\\") {
		return "/checkout"
	}
	return p
}
