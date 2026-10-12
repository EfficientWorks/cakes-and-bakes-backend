package domain

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/stripe/stripe-go/v83"

	"bakehouse/services/payments/internal/store"
	"bakehouse/services/payments/paymentsapi"
)

// StripeProcessor implements paymentsapi.Processor against Stripe
// PaymentIntents.
//
// The storefront pays through Stripe Checkout (a Stripe-hosted page), so the
// path that matters is VerifyIntent: the customer comes back with a
// PaymentIntent id and it is re-read from Stripe before any order exists.
// Capture remains for a client that tokenises a card itself.
//
// The PaymentIntent id is stored as provider_payment_id on payments and
// orders. Stripe ids start with "pi_"; a counter payment's placeholder starts
// "noop_" and is never refunded through Stripe.
type StripeProcessor struct {
	client *stripe.Client
	store  *store.Store
}

func NewStripeProcessor(client *stripe.Client, store *store.Store) *StripeProcessor {
	return &StripeProcessor{client: client, store: store}
}

func (p *StripeProcessor) Capture(ctx context.Context, amountCents int64, currency, paymentMethodID, idempotencyKey string) (*paymentsapi.PaymentResult, error) {
	// Staff-taken orders (phone/manual, the counter) pass a method name, not
	// a card. Nothing goes to Stripe; the money is taken in person. The
	// "noop_" prefix is what refundableAtProvider skips.
	switch paymentMethodID {
	case paymentsapi.MethodManualCounter, paymentsapi.MethodCash:
		ref := "noop_" + paymentMethodID + "_" + idempotencyKey
		now := time.Now()
		if _, err := p.store.CreatePayment(ctx, paymentsapi.Payment{
			Method: paymentMethodID, AmountCents: amountCents, Currency: currency,
			ProviderPaymentID: &ref, Status: paymentsapi.StatusCaptured, CapturedAt: &now,
		}); err != nil {
			slog.Error("could not record counter payment", "ref", ref, "error", err)
		}
		return &paymentsapi.PaymentResult{ProviderRef: ref, Status: paymentsapi.StatusCaptured}, nil
	}

	params := &stripe.PaymentIntentCreateParams{
		Amount:        stripe.Int64(amountCents),
		Currency:      stripe.String(strings.ToLower(currency)),
		PaymentMethod: stripe.String(paymentMethodID),
		Confirm:       stripe.Bool(true),
		CaptureMethod: stripe.String("automatic"),
		Description:   stripe.String(statementDescription),
		AutomaticPaymentMethods: &stripe.PaymentIntentCreateAutomaticPaymentMethodsParams{
			Enabled:        stripe.Bool(true),
			AllowRedirects: stripe.String("never"),
		},
	}
	params.IdempotencyKey = stripe.String(idempotencyKey)

	pi, err := p.client.V1PaymentIntents.Create(ctx, params)
	if err != nil {
		reason := err.Error()
		_, _ = p.store.CreatePayment(ctx, paymentsapi.Payment{
			Method: paymentsapi.MethodCard, AmountCents: amountCents, Currency: currency,
			ProviderPaymentMethodID: &paymentMethodID, Status: paymentsapi.StatusFailed, FailureReason: &reason,
		})
		return nil, err
	}

	status := paymentsapi.StatusPending
	var capturedAt *time.Time
	if pi.Status == stripe.PaymentIntentStatusSucceeded {
		status = paymentsapi.StatusCaptured
		now := time.Now()
		capturedAt = &now
	}
	if _, err := p.store.CreatePayment(ctx, paymentsapi.Payment{
		Method: paymentsapi.MethodCard, AmountCents: amountCents, Currency: currency,
		ProviderPaymentID: &pi.ID, ProviderPaymentMethodID: &paymentMethodID,
		Status: status, CapturedAt: capturedAt,
	}); err != nil {
		// Stripe already has the money; losing our audit row is logged, not
		// a reason to fail checkout.
		slog.Error("could not record payment", "intent", pi.ID, "error", err)
	}

	// 3-D Secure: the browser runs the challenge with the client secret and
	// the order is re-requested with the intent id (VerifyIntent).
	if pi.Status == stripe.PaymentIntentStatusRequiresAction {
		return nil, &paymentsapi.AuthenticationRequired{PaymentIntentID: pi.ID, ClientSecret: pi.ClientSecret}
	}
	if status != paymentsapi.StatusCaptured {
		return nil, fmt.Errorf("payment could not be completed (status=%s)", pi.Status)
	}
	return &paymentsapi.PaymentResult{ProviderRef: pi.ID, Status: paymentsapi.StatusCaptured}, nil
}

// VerifyIntent finishes a payment made on Stripe Checkout (or after a 3-D
// Secure challenge). The intent id comes from the browser, so its status and
// amount are read back from Stripe rather than believed: otherwise anyone
// could present a one-cent payment for an expensive order.
func (p *StripeProcessor) VerifyIntent(ctx context.Context, paymentIntentID string, amountCents int64) (*paymentsapi.PaymentResult, error) {
	pi, err := p.client.V1PaymentIntents.Retrieve(ctx, paymentIntentID, nil)
	if err != nil {
		return nil, err
	}
	if pi.Status != stripe.PaymentIntentStatusSucceeded {
		if pi.Status == stripe.PaymentIntentStatusRequiresAction {
			return nil, &paymentsapi.AuthenticationRequired{PaymentIntentID: pi.ID, ClientSecret: pi.ClientSecret}
		}
		return nil, fmt.Errorf("payment not completed (status=%s)", pi.Status)
	}
	if pi.Amount != amountCents {
		return nil, fmt.Errorf("payment amount %d does not match the order total %d", pi.Amount, amountCents)
	}

	// A retried order placement verifies the same intent twice; the unique
	// index on the payment id keeps one row, and the second insert's error
	// is expected.
	if _, err := p.store.GetPaymentByProviderID(ctx, pi.ID); err != nil {
		now := time.Now()
		if _, err := p.store.CreatePayment(ctx, paymentsapi.Payment{
			Method: paymentsapi.MethodCard, AmountCents: pi.Amount, Currency: strings.ToUpper(string(pi.Currency)),
			ProviderPaymentID: &pi.ID, Status: paymentsapi.StatusCaptured, CapturedAt: &now,
		}); err != nil {
			slog.Error("could not record verified payment", "intent", pi.ID, "error", err)
		}
	}
	return &paymentsapi.PaymentResult{ProviderRef: pi.ID, Status: paymentsapi.StatusCaptured}, nil
}

func (p *StripeProcessor) LinkOrder(ctx context.Context, providerRef, orderID, branchID string) {
	_ = p.store.LinkPaymentToOrder(ctx, providerRef, orderID, branchID)
}

// RecordPending books money owed but not yet taken (cash on delivery). No
// provider is called.
func (p *StripeProcessor) RecordPending(ctx context.Context, orderID, branchID, method string, amountCents int64, currency string) {
	if _, err := p.store.CreatePayment(ctx, paymentsapi.Payment{
		OrderID: &orderID, BranchID: &branchID,
		Method: method, AmountCents: amountCents, Currency: currency,
		Status: paymentsapi.StatusPending,
	}); err != nil {
		slog.Error("could not record pending payment", "order", orderID, "error", err)
	}
}

func (p *StripeProcessor) Refund(ctx context.Context, orderID, providerRef string, amountCents int64, reason, issuedByStaffID string) (*paymentsapi.PaymentResult, error) {
	params := &stripe.RefundCreateParams{
		PaymentIntent: stripe.String(providerRef),
		Amount:        stripe.Int64(amountCents),
		Reason:        stripe.String(string(stripe.RefundReasonRequestedByCustomer)),
	}
	if reason != "" {
		params.AddMetadata("reason", reason)
	}
	rf, err := p.client.V1Refunds.Create(ctx, params)
	if err != nil {
		return nil, err
	}

	status := paymentsapi.RefundStatusPending
	switch rf.Status {
	case stripe.RefundStatusSucceeded:
		status = paymentsapi.RefundStatusSucceeded
	case stripe.RefundStatusFailed, stripe.RefundStatusCanceled:
		status = paymentsapi.RefundStatusFailed
	}

	var paymentID string
	if payment, err := p.store.GetPaymentByProviderID(ctx, providerRef); err == nil {
		paymentID = payment.ID
	}
	var issuedBy *string
	if issuedByStaffID != "" {
		issuedBy = &issuedByStaffID
	}
	if _, err := p.store.CreateRefund(ctx, paymentsapi.Refund{
		PaymentID: paymentID, OrderID: orderID, AmountCents: amountCents, Reason: reason,
		ProviderRefundID: &rf.ID, Status: status, IssuedByUserID: issuedBy,
	}); err != nil {
		slog.Error("could not record refund", "refund", rf.ID, "error", err)
	}

	resultStatus := "refunded"
	if status != paymentsapi.RefundStatusSucceeded {
		resultStatus = "pending"
	}
	return &paymentsapi.PaymentResult{ProviderRef: rf.ID, Status: resultStatus}, nil
}
