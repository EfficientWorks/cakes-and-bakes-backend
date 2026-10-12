package paymentsapi

import (
	"context"
	"strings"
	"time"
)

/*
 * Card payments through a provider, as the rest of the backend sees them.
 *
 * Orders never talks to Stripe directly. It asks this seam to open a hosted
 * payment page for a counter order (a payment link), to refund what an order
 * was charged, and it is told by events when a webhook says a link was paid
 * or a refund settled.
 */

// Card providers. Manual is a card run on the store's own terminal: recorded,
// but there is nothing to refund through an API.
const (
	ProviderStripe = "stripe"
	ProviderManual = "manual"
)

// ProviderOf names the provider that issued a payment id. Stripe
// PaymentIntents start "pi_"; a counter placeholder ("noop_…") or an empty
// ref has no provider, so there is nothing to refund through an API.
func ProviderOf(paymentRef string) string {
	if strings.HasPrefix(paymentRef, "pi_") {
		return ProviderStripe
	}
	return ""
}

// Payment link statuses.
const (
	LinkOpen      = "open"
	LinkPaid      = "paid"
	LinkExpired   = "expired"
	LinkCancelled = "cancelled"
)

// PaymentLink is a hosted payment page sent to a customer for one order.
type PaymentLink struct {
	ID            string
	OrderID       string
	BillID        *string
	BranchID      string
	Provider      string
	SessionID     string // Stripe Checkout Session id
	URL           string
	AmountCents   int64
	Currency      string
	CustomerEmail string
	Status        string
	PaymentRef    *string // the provider payment that paid it
	SentCount     int
	CreatedAt     time.Time
	PaidAt        *time.Time
}

// CreateLinkInput opens a payment link.
type CreateLinkInput struct {
	OrderID       string
	BranchID      string
	BillID        *string
	Provider      string
	AmountCents   int64
	Currency      string
	CustomerEmail string
	Reference     string // the order reference, shown on the provider's page
	CreatedBy     string
	// ReplaceLinkID is the order's open link this one replaces. It is expired
	// only after the new page has opened, so a failure keeps the old one.
	ReplaceLinkID string
}

// KnownCharge is a provider payment the caller knows an order was paid with
// (a website order's payment id). The ledger is backfilled from it when it
// has no row of its own, so older orders can still be refunded.
type KnownCharge struct {
	PaymentRef  string
	AmountCents int64
	Currency    string
}

// RefundInput asks for money back on an order's provider payments.
type RefundInput struct {
	OrderID  string
	BranchID string
	// Provider limits the refund to one provider's payments; "" means any.
	Provider    string
	AmountCents int64
	Reason      string
	IssuedBy    string
	Known       []KnownCharge
}

// Refund line statuses (per provider refund).
const (
	RefundLineSucceeded = "succeeded"
	RefundLinePending   = "pending"
)

// RefundLine is one refund at a provider. A split order paid with two cards
// is refunded with two lines.
type RefundLine struct {
	Provider    string
	PaymentRef  string
	RefundRef   string
	AmountCents int64
	Status      string // succeeded | pending (settles by webhook)
}

// RefundOutcome is what a refund did.
type RefundOutcome struct {
	RefundedCents int64
	// RemainingCents is what can still be refunded on those payments.
	RemainingCents int64
	Lines          []RefundLine
}

// Pending reports whether any line still waits on the provider.
func (o *RefundOutcome) Pending() bool {
	for _, l := range o.Lines {
		if l.Status == RefundLinePending {
			return true
		}
	}
	return false
}

// Events raised from provider webhooks.
const (
	EventPaymentCompleted   = "payment.completed"
	EventPaymentLinkExpired = "payment.link_expired"
	EventRefundCompleted    = "refund.completed"
	EventRefundFailed       = "refund.failed"
)

// Event is something a provider webhook reported about an order's money.
type Event struct {
	Type        string
	OrderID     string
	BranchID    string
	Provider    string
	AmountCents int64
	PaymentRef  string
	RefundRef   string
	Link        *PaymentLink // payment.* events
}

// EventHandler receives webhook events. Called after the payments ledger is
// updated; it must not fail the webhook.
type EventHandler func(ctx context.Context, e Event)

// Cards is the seam orders uses for provider card payments.
type Cards interface {
	// Providers are the card providers configured on this server.
	Providers() []string

	// CreateLink opens a hosted payment page. An open link for the order must
	// be named in ReplaceLinkID; it is expired once the new one has opened.
	CreateLink(ctx context.Context, in CreateLinkInput) (*PaymentLink, error)
	// CancelLink expires a link at the provider so it can no longer be paid,
	// and marks it cancelled. A link that is no longer open is left alone.
	CancelLink(ctx context.Context, linkID string) error
	// RefreshLink asks the provider whether an open link has been paid (or
	// has expired) and records it — the same as the webhook would — then
	// returns the link as it now stands. Covers a webhook that never came.
	RefreshLink(ctx context.Context, linkID string) (*PaymentLink, error)
	// MarkLinkResent counts another send of the same link.
	MarkLinkResent(ctx context.Context, linkID string) (*PaymentLink, error)
	// OpenLink is the order's open link, or nil.
	OpenLink(ctx context.Context, orderID string) (*PaymentLink, error)
	// LatestLink is the order's most recent link in any status, or nil.
	LatestLink(ctx context.Context, orderID string) (*PaymentLink, error)

	// RefundableCents is what the order's provider payments can still give back.
	RefundableCents(ctx context.Context, orderID, provider string, known []KnownCharge) (int64, error)
	// Refund gives money back across the order's provider payments, largest
	// remaining first. Fails without refunding when more is asked than can be.
	Refund(ctx context.Context, in RefundInput) (*RefundOutcome, error)
}
