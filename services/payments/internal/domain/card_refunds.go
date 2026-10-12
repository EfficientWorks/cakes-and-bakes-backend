package domain

import (
	"context"
	"fmt"
	"log/slog"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/money"
	"bakehouse/services/payments/internal/store"
	"bakehouse/services/payments/paymentsapi"
)

/*
 * Refunds across an order's Stripe payments.
 *
 * An order can carry several Stripe payments: a split bill paid with two
 * cards, or a link paid on top of a website charge. Each payment can only give
 * back what it took, so a refund is spread over them — largest remaining
 * first, which keeps the number of refunds (and of lines on the customer's
 * statement) as small as possible.
 *
 * Cash and manual card payments are not in this ledger at all: there is
 * nothing to send back through an API, and the till hands the money over.
 */

// refundSources backfills the known charges, then lists what can be refunded.
func (s *Service) refundSources(ctx context.Context, orderID, branchID string, known []paymentsapi.KnownCharge) ([]store.RefundSource, error) {
	for _, k := range known {
		if paymentsapi.ProviderOf(k.PaymentRef) == "" || k.AmountCents <= 0 {
			continue
		}
		if err := s.store.EnsureCapturedPayment(ctx, k.PaymentRef, orderID, branchID, k.AmountCents, k.Currency); err != nil {
			slog.Warn("refund: could not backfill payment", "order", orderID, "payment", k.PaymentRef, "error", err)
		}
	}
	return s.store.RefundSources(ctx, orderID)
}

// RefundableCents is what the order's Stripe payments can still give back.
// provider is accepted for the Cards contract; only Stripe moves money.
func (s *Service) RefundableCents(ctx context.Context, orderID, provider string, known []paymentsapi.KnownCharge) (int64, error) {
	if provider != "" && provider != paymentsapi.ProviderStripe {
		return 0, nil
	}
	sources, err := s.refundSources(ctx, orderID, "", known)
	if err != nil {
		return 0, apperr.Internal(err)
	}
	var total int64
	for _, src := range sources {
		total += src.RemainingCents
	}
	return total, nil
}

// Refund gives money back across the order's Stripe payments, largest
// remaining first. It refuses without refunding anything when more is asked
// for than the payments can give.
func (s *Service) Refund(ctx context.Context, in paymentsapi.RefundInput) (*paymentsapi.RefundOutcome, error) {
	if in.AmountCents <= 0 {
		return &paymentsapi.RefundOutcome{}, nil
	}
	if in.Provider != "" && in.Provider != paymentsapi.ProviderStripe {
		return nil, apperr.Validation("Card refunds go back through Stripe only.")
	}
	sources, err := s.refundSources(ctx, in.OrderID, in.BranchID, in.Known)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	var available int64
	currency := ""
	for _, src := range sources {
		available += src.RemainingCents
		currency = src.Currency
	}
	if available < in.AmountCents {
		return nil, apperr.New("REFUND_EXCEEDS_PAID", apperr.CategoryBusinessRule,
			fmt.Sprintf("Only %s can be refunded to this order's card payments; %s was asked for.",
				money.Format(available, currency), money.Format(in.AmountCents, currency))).
			WithMeta(map[string]any{"refundableCents": available, "requestedCents": in.AmountCents})
	}
	if s.refunds == nil {
		return nil, errProviderNotConfigured()
	}

	out := &paymentsapi.RefundOutcome{}
	left := in.AmountCents
	for _, src := range sources {
		if left == 0 {
			break
		}
		take := min(left, src.RemainingCents)
		res, err := s.refunds.Refund(ctx, in.OrderID, src.PaymentRef, take, in.Reason, in.IssuedBy)
		if err != nil {
			// What went through stays recorded in out; the caller is told how
			// much did move before the failure.
			return out, apperr.Wrap(err, "REFUND_FAILED", apperr.CategoryUpstream, "Stripe did not accept the refund.").
				WithMeta(map[string]any{"refundedCents": out.RefundedCents})
		}
		status := paymentsapi.RefundLineSucceeded
		if res.Status != "refunded" {
			status = paymentsapi.RefundLinePending
		}
		out.Lines = append(out.Lines, paymentsapi.RefundLine{
			Provider: paymentsapi.ProviderStripe, PaymentRef: src.PaymentRef, RefundRef: res.ProviderRef,
			AmountCents: take, Status: status,
		})
		out.RefundedCents += take
		left -= take
		_ = s.store.SyncPaymentRefundStatus(ctx, src.PaymentRef)
	}
	out.RemainingCents = available - out.RefundedCents
	return out, nil
}

// refundSettled is a Stripe refund webhook: the ledger is updated and, the
// first time a refund reaches a final status, orders is told.
func (s *Service) refundSettled(ctx context.Context, refundID, status string) {
	changed, err := s.store.SetRefundStatus(ctx, refundID, status)
	if err != nil || !changed || status == paymentsapi.RefundStatusPending {
		return
	}
	ro, err := s.store.GetRefundOrder(ctx, refundID)
	if err != nil {
		return
	}
	if ro.PaymentRef != "" {
		_ = s.store.SyncPaymentRefundStatus(ctx, ro.PaymentRef)
	}
	eventType := paymentsapi.EventRefundCompleted
	if status == paymentsapi.RefundStatusFailed {
		eventType = paymentsapi.EventRefundFailed
	}
	s.emit(ctx, paymentsapi.Event{
		Type: eventType, OrderID: ro.OrderID, BranchID: ro.BranchID, Provider: paymentsapi.ProviderStripe,
		AmountCents: ro.AmountCents, PaymentRef: ro.PaymentRef, RefundRef: refundID,
	})
}
