package paymentsapi

import (
	"context"
	"errors"
)

// PaymentResult is what a successful authorization/capture returns.
type PaymentResult struct {
	ProviderRef string // Stripe PaymentIntent id (pi_…) or refund id (re_…)
	Status      string // captured | pending
}

// ErrAuthenticationRequired reports that the card needs the customer to
// authenticate — Strong Customer Authentication (SCA/3-D Secure) — before
// the money can move.
//
// This is not a failure. It is the normal path for a great many cards, and
// in the UK (PSD2) most online card payments are challenged. The browser runs
// the challenge with the client secret and the order is requested again with
// the intent id, which VerifyIntent then checks.
var ErrAuthenticationRequired = errors.New("payment requires customer authentication")

// AuthenticationRequired carries what the browser needs to run the challenge.
// Returned alongside ErrAuthenticationRequired so callers can answer with a
// client secret rather than an apology.
type AuthenticationRequired struct {
	// PaymentIntentID identifies the payment to resume once the challenge is
	// done. The client sends it back and the server verifies it rather than
	// creating a second one.
	PaymentIntentID string
	// ClientSecret authorises the browser to act on that one payment, and
	// nothing else. Safe to hand out; it is not an API key.
	ClientSecret string
}

func (a *AuthenticationRequired) Error() string { return ErrAuthenticationRequired.Error() }
func (a *AuthenticationRequired) Unwrap() error { return ErrAuthenticationRequired }

// Processor is the seam the payments module fills in (Stripe). Ordering depends only on this interface so checkout can be
// built and tested before the payments module exists — the phased-build
// pattern used throughout this backend (see geo.Geocoder, email.Emailer).
//
// Capture happens before the order row exists (checkout never creates an
// order for a payment that failed), so it can't be given an order id up
// front — LinkOrder attaches one immediately afterward, best-effort, purely
// for the payments module's own audit trail. Its failure never fails
// checkout: the order and the money movement have already both succeeded
// by the time it's called.
type Processor interface {
	// Capture authorizes and captures amountCents in one step (BRD: card
	// payment is captured at order placement, PAY-01).
	Capture(ctx context.Context, amountCents int64, currency, paymentMethodID, idempotencyKey string) (*PaymentResult, error)
	// VerifyIntent finishes a payment the customer has just authenticated.
	//
	// It must confirm two things against the provider rather than trusting
	// the caller: that the intent really succeeded, and that it was for
	// amountCents. The intent id arrives from the browser, so without the
	// amount check a customer could authenticate a one-cent intent and
	// present it for a hundred-pound order.
	VerifyIntent(ctx context.Context, paymentIntentID string, amountCents int64) (*PaymentResult, error)
	// LinkOrder records which order a successful capture belongs to, once
	// that order exists. Best-effort — see interface doc above.
	LinkOrder(ctx context.Context, providerRef, orderID, branchID string)
	// RecordPending writes a payment row for money that is owed but not yet
	// taken — cash on delivery, and nothing else today.
	//
	// It exists because COD is the one path where the order precedes the
	// money, so there is no capture to link. Writing the row anyway keeps
	// every pound the business is owed in one ledger, which is what makes
	// day-close reconciliation a query rather than a reconstruction.
	// Best-effort, like LinkOrder: a failure here must not undo a placed
	// order, and the order carries its own payment_status regardless.
	RecordPending(ctx context.Context, orderID, branchID, method string, amountCents int64, currency string)
	// Refund returns amountCents to the original payment method.
	Refund(ctx context.Context, orderID, providerRef string, amountCents int64, reason, issuedByStaffID string) (*PaymentResult, error)
}

// NoopProcessor marks every payment captured without calling a real
// provider. Used when no Stripe key is configured — orders
// created against it are real orders with real slot reservations; only the
// money movement is stubbed, clearly marked in payment_status metadata.
type NoopProcessor struct{}

func (NoopProcessor) Capture(ctx context.Context, amountCents int64, currency, paymentMethodID, idempotencyKey string) (*PaymentResult, error) {
	return &PaymentResult{ProviderRef: "noop_" + idempotencyKey, Status: StatusCaptured}, nil
}

// VerifyIntent accepts whatever it is given: with no provider there is no
// intent to check, and a stub that rejected would make the authenticated
// retry path untestable without Stripe configured.
func (NoopProcessor) VerifyIntent(ctx context.Context, paymentIntentID string, amountCents int64) (*PaymentResult, error) {
	return &PaymentResult{ProviderRef: paymentIntentID, Status: StatusCaptured}, nil
}

func (NoopProcessor) LinkOrder(ctx context.Context, providerRef, orderID, branchID string) {
}

func (NoopProcessor) RecordPending(ctx context.Context, orderID, branchID, method string, amountCents int64, currency string) {
}

func (NoopProcessor) Refund(ctx context.Context, orderID, providerRef string, amountCents int64, reason, issuedByStaffID string) (*PaymentResult, error) {
	return &PaymentResult{ProviderRef: providerRef, Status: "refunded"}, nil
}
