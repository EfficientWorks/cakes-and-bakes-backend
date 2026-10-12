package domain

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/stripe/stripe-go/v83"

	"bakehouse/pkg/apperr"
	"bakehouse/services/payments/internal/store"
	"bakehouse/services/payments/paymentsapi"
)

/*
 * Payment links: a hosted payment page sent to a counter customer by email.
 *
 * The till asks for one; Stripe opens a Checkout Session for the amount owed;
 * the customer pays on it; the webhook says so (HandleStripeWebhook), and the
 * EventPaymentCompleted event lets orders record the payment.
 *
 * One open link per order is enforced in the database. Expiring a session at
 * Stripe is what stops a customer paying through an old link, so cancelling
 * always calls Stripe first.
 */

// linkLifetime: how long a link stays payable — the most Stripe allows for a
// Checkout Session.
const linkLifetime = 24 * time.Hour

// linkReturnURL is where Stripe sends the customer after paying or backing
// out: a page on the storefront, never a URL from the request.
func (s *Service) linkReturnURL(ctx context.Context, reference string, paid bool) string {
	page := "/payment/cancelled"
	if paid {
		page = "/payment/complete"
	}
	return s.shopBase(ctx) + page + "?order=" + url.QueryEscape(reference)
}

// CreateLink opens a hosted payment page at Stripe and saves it.
func (s *Service) CreateLink(ctx context.Context, in paymentsapi.CreateLinkInput) (*paymentsapi.PaymentLink, error) {
	if err := s.requireStripe(in.Provider); err != nil {
		return nil, err
	}
	if in.AmountCents <= 0 {
		return nil, apperr.Validation("There is nothing left to pay on this order.")
	}
	if in.Currency == "" {
		return nil, apperr.Validation("currency is required.")
	}

	row := store.LinkRow{PaymentLink: paymentsapi.PaymentLink{
		OrderID: in.OrderID, BillID: in.BillID, BranchID: in.BranchID, Provider: paymentsapi.ProviderStripe,
		AmountCents: in.AmountCents, Currency: strings.ToUpper(in.Currency), CustomerEmail: in.CustomerEmail,
	}}
	if in.CreatedBy != "" {
		row.CreatedBy = &in.CreatedBy
	}

	if err := s.openStripeLink(ctx, in, &row); err != nil {
		return nil, apperr.Wrap(err, "PAYMENT_LINK_FAILED", apperr.CategoryUpstream,
			"The payment link could not be created. Please try again.")
	}
	// The new page is open; only now does the old one stop working.
	if in.ReplaceLinkID != "" {
		if err := s.CancelLink(ctx, in.ReplaceLinkID); err != nil {
			s.expireAtStripe(context.WithoutCancel(ctx), row.SessionID)
			return nil, err
		}
	}

	link, err := s.store.CreateLink(ctx, row)
	if err != nil {
		// Opened at Stripe but not saved: close it there so it cannot be paid
		// without the order ever hearing about it.
		s.expireAtStripe(context.WithoutCancel(ctx), row.SessionID)
		return nil, apperr.Wrap(err, "PAYMENT_LINK_FAILED", apperr.CategoryConflict,
			"This order already has an open payment link. Cancel it first.")
	}
	return link, nil
}

func (s *Service) openStripeLink(ctx context.Context, in paymentsapi.CreateLinkInput, row *store.LinkRow) error {
	params := &stripe.CheckoutSessionCreateParams{
		Mode: stripe.String(string(stripe.CheckoutSessionModePayment)),
		LineItems: []*stripe.CheckoutSessionCreateLineItemParams{{
			Quantity: stripe.Int64(1),
			PriceData: &stripe.CheckoutSessionCreateLineItemPriceDataParams{
				Currency:   stripe.String(strings.ToLower(row.Currency)),
				UnitAmount: stripe.Int64(in.AmountCents),
				ProductData: &stripe.CheckoutSessionCreateLineItemPriceDataProductDataParams{
					Name: stripe.String(checkoutLabel + " #" + in.Reference),
				},
			},
		}},
		SuccessURL: stripe.String(s.linkReturnURL(ctx, in.Reference, true)),
		CancelURL:  stripe.String(s.linkReturnURL(ctx, in.Reference, false)),
		ExpiresAt:  stripe.Int64(time.Now().Add(linkLifetime).Unix()),
		PaymentIntentData: &stripe.CheckoutSessionCreatePaymentIntentDataParams{
			Description: stripe.String(statementDescription),
		},
	}
	if in.CustomerEmail != "" {
		params.CustomerEmail = stripe.String(in.CustomerEmail)
	}
	// Lets anyone reading the Stripe dashboard find the order.
	params.AddMetadata("order_id", in.OrderID)
	params.AddMetadata("order_reference", in.Reference)
	params.PaymentIntentData.AddMetadata("order_id", in.OrderID)

	cs, err := s.stripe.V1CheckoutSessions.Create(ctx, params)
	if err != nil {
		return err
	}
	row.SessionID, row.URL = cs.ID, cs.URL
	return nil
}

// expireAtStripe makes a link unpayable. Best effort: a session that already
// expired or was paid answers with an error, which is the outcome wanted.
func (s *Service) expireAtStripe(ctx context.Context, sessionID string) {
	if s.stripe == nil {
		return
	}
	if _, err := s.stripe.V1CheckoutSessions.Expire(ctx, sessionID, nil); err != nil {
		slog.Warn("payment link: could not expire at Stripe", "session", sessionID, "error", err)
	}
}

// CancelLink expires a link at Stripe and marks it cancelled.
func (s *Service) CancelLink(ctx context.Context, linkID string) error {
	link, err := s.store.GetLink(ctx, linkID)
	if err != nil {
		return notFoundOr(err, "Payment link not found.")
	}
	if link.Status != paymentsapi.LinkOpen {
		return nil
	}
	// Paid already (its webhook not in yet): record the payment and say so,
	// rather than cancelling a link the customer has paid.
	if s.syncLink(ctx, link) == paymentsapi.LinkPaid {
		return errLinkAlreadyPaid()
	}
	s.expireAtStripe(ctx, link.SessionID)
	if _, err := s.store.CloseLink(ctx, link.ID, paymentsapi.LinkCancelled); err != nil {
		return apperr.Internal(err)
	}
	return nil
}

// RefreshLink brings an open link up to date with Stripe.
func (s *Service) RefreshLink(ctx context.Context, linkID string) (*paymentsapi.PaymentLink, error) {
	link, err := s.store.GetLink(ctx, linkID)
	if err != nil {
		return nil, notFoundOr(err, "Payment link not found.")
	}
	if link.Status != paymentsapi.LinkOpen || s.syncLink(ctx, link) == paymentsapi.LinkOpen {
		return link, nil
	}
	link, err = s.store.GetLink(ctx, linkID)
	if err != nil {
		return nil, notFoundOr(err, "Payment link not found.")
	}
	return link, nil
}

// syncLink reads an open link's session from Stripe and records a payment or
// an expiry the same way the webhook does (each only once, whichever comes
// first). Returns the link's status after; open when Stripe can't be asked or
// nothing has happened. This is what heals a webhook that never arrived.
func (s *Service) syncLink(ctx context.Context, link *paymentsapi.PaymentLink) string {
	if s.stripe == nil || link.Provider != paymentsapi.ProviderStripe {
		return link.Status
	}
	cs, err := s.stripe.V1CheckoutSessions.Retrieve(ctx, link.SessionID, nil)
	if err != nil {
		slog.Warn("payment link: could not read session", "link", link.ID, "error", err)
		return link.Status
	}
	if cs.PaymentStatus == stripe.CheckoutSessionPaymentStatusPaid && cs.PaymentIntent != nil {
		s.linkPaid(ctx, link, cs.PaymentIntent.ID, cs.AmountTotal)
		return paymentsapi.LinkPaid
	}
	if cs.Status == stripe.CheckoutSessionStatusExpired {
		s.linkExpired(ctx, link)
		return paymentsapi.LinkExpired
	}
	return link.Status
}

func errLinkAlreadyPaid() error {
	return apperr.New("LINK_ALREADY_PAID", apperr.CategoryConflict,
		"The customer has already paid this link. The payment has been recorded on the order.")
}

func (s *Service) MarkLinkResent(ctx context.Context, linkID string) (*paymentsapi.PaymentLink, error) {
	if err := s.store.IncrementLinkSent(ctx, linkID); err != nil {
		return nil, apperr.Internal(err)
	}
	link, err := s.store.GetLink(ctx, linkID)
	if err != nil {
		return nil, notFoundOr(err, "Payment link not found.")
	}
	return link, nil
}

func (s *Service) OpenLink(ctx context.Context, orderID string) (*paymentsapi.PaymentLink, error) {
	return nilIfNotFound(s.store.GetOpenLinkByOrder(ctx, orderID))
}

func (s *Service) LatestLink(ctx context.Context, orderID string) (*paymentsapi.PaymentLink, error) {
	return nilIfNotFound(s.store.GetLatestLinkByOrder(ctx, orderID))
}

// linkPaid records a link's payment in the ledger and tells orders, once.
func (s *Service) linkPaid(ctx context.Context, link *paymentsapi.PaymentLink, paymentRef string, amountCents int64) {
	first, err := s.store.MarkLinkPaid(ctx, link.ID, paymentRef, time.Now())
	if err != nil {
		slog.Error("payment link: could not mark paid", "link", link.ID, "error", err)
		return
	}
	if !first {
		return // a repeated webhook
	}
	if amountCents <= 0 {
		amountCents = link.AmountCents
	}
	if err := s.store.EnsureCapturedPayment(ctx, paymentRef, link.OrderID, link.BranchID, amountCents, link.Currency); err != nil {
		slog.Error("payment link: could not record payment", "link", link.ID, "payment", paymentRef, "error", err)
	}
	link.Status, link.PaymentRef = paymentsapi.LinkPaid, &paymentRef
	s.emit(ctx, paymentsapi.Event{
		Type: paymentsapi.EventPaymentCompleted, OrderID: link.OrderID, BranchID: link.BranchID,
		Provider: link.Provider, AmountCents: amountCents, PaymentRef: paymentRef, Link: link,
	})
}

// linkExpired marks a link Stripe expired after its 24 hours.
func (s *Service) linkExpired(ctx context.Context, link *paymentsapi.PaymentLink) {
	closed, err := s.store.CloseLink(ctx, link.ID, paymentsapi.LinkExpired)
	if err != nil || !closed {
		return
	}
	link.Status = paymentsapi.LinkExpired
	s.emit(ctx, paymentsapi.Event{
		Type: paymentsapi.EventPaymentLinkExpired, OrderID: link.OrderID, BranchID: link.BranchID,
		Provider: link.Provider, AmountCents: link.AmountCents, Link: link,
	})
}

func nilIfNotFound(l *paymentsapi.PaymentLink, err error) (*paymentsapi.PaymentLink, error) {
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return l, nil
}

func notFoundOr(err error, msg string) error {
	if errors.Is(err, store.ErrNotFound) {
		return apperr.NotFound(msg)
	}
	return apperr.Internal(err)
}
