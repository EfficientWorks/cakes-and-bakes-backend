package domain

import (
	"context"
	"encoding/json"

	"github.com/stripe/stripe-go/v83"
	"github.com/stripe/stripe-go/v83/webhook"

	"bakehouse/pkg/apperr"
	"bakehouse/services/payments/paymentsapi"
)

// HandleStripeWebhook verifies Stripe's signature and keeps the payment and
// refund ledger in step. Website orders are paid synchronously at checkout,
// so for them this is a correction path; for POS payment links it is how the
// order hears that the customer paid.
func (s *Service) HandleStripeWebhook(ctx context.Context, payload []byte, signatureHeader string) error {
	if s.stripeWebhookSecret == "" {
		return apperr.New("WEBHOOK_NOT_CONFIGURED", apperr.CategoryUpstream, "Stripe webhook secret is not configured.")
	}
	event, err := webhook.ConstructEventWithOptions(payload, signatureHeader, s.stripeWebhookSecret,
		webhook.ConstructEventOptions{IgnoreAPIVersionMismatch: true})
	if err != nil {
		return apperr.New("INVALID_SIGNATURE", apperr.CategoryAuthentication, "Webhook signature verification failed.")
	}

	switch event.Type {
	case "payment_intent.succeeded", "payment_intent.payment_failed":
		s.onPaymentIntent(ctx, event)
	case "refund.created", "refund.updated", "refund.failed":
		s.onRefund(ctx, event)
	case "checkout.session.completed", "checkout.session.async_payment_succeeded":
		s.onCheckoutPaid(ctx, event)
	case "checkout.session.expired":
		s.onCheckoutExpired(ctx, event)
	}
	return nil
}

func (s *Service) onPaymentIntent(ctx context.Context, event stripe.Event) {
	var pi stripe.PaymentIntent
	if err := json.Unmarshal(event.Data.Raw, &pi); err != nil || pi.ID == "" {
		return
	}
	status := paymentsapi.StatusCaptured
	var reason *string
	if event.Type == "payment_intent.payment_failed" {
		status = paymentsapi.StatusFailed
		if pi.LastPaymentError != nil {
			msg := pi.LastPaymentError.Msg
			reason = &msg
		}
	}
	_ = s.store.UpdatePaymentStatus(ctx, pi.ID, status, reason)
}

func (s *Service) onRefund(ctx context.Context, event stripe.Event) {
	var rf stripe.Refund
	if err := json.Unmarshal(event.Data.Raw, &rf); err != nil || rf.ID == "" {
		return
	}
	s.refundSettled(ctx, rf.ID, refundStatus(rf.Status))
}

// onCheckoutPaid acts only on sessions opened as payment links; website
// checkout sessions are completed by the storefront creating the order.
func (s *Service) onCheckoutPaid(ctx context.Context, event stripe.Event) {
	var cs stripe.CheckoutSession
	if err := json.Unmarshal(event.Data.Raw, &cs); err != nil || cs.ID == "" {
		return
	}
	if cs.PaymentStatus != stripe.CheckoutSessionPaymentStatusPaid || cs.PaymentIntent == nil {
		return
	}
	if link, err := s.store.GetLinkBySession(ctx, paymentsapi.ProviderStripe, cs.ID); err == nil {
		s.linkPaid(ctx, link, cs.PaymentIntent.ID, cs.AmountTotal)
	}
}

func (s *Service) onCheckoutExpired(ctx context.Context, event stripe.Event) {
	var cs stripe.CheckoutSession
	if err := json.Unmarshal(event.Data.Raw, &cs); err != nil || cs.ID == "" {
		return
	}
	if link, err := s.store.GetLinkBySession(ctx, paymentsapi.ProviderStripe, cs.ID); err == nil {
		s.linkExpired(ctx, link)
	}
}

// refundStatus maps Stripe's refund status onto the ledger's.
func refundStatus(st stripe.RefundStatus) string {
	switch st {
	case stripe.RefundStatusSucceeded:
		return paymentsapi.RefundStatusSucceeded
	case stripe.RefundStatusFailed, stripe.RefundStatusCanceled:
		return paymentsapi.RefundStatusFailed
	}
	return paymentsapi.RefundStatusPending
}
