package domain

import (
	"context"
	"strings"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/money"
	"bakehouse/services/orders/ordersapi"
	"bakehouse/services/payments/paymentsapi"
)

/*
 * How a return's money goes back.
 *
 * POST /pos/orders/{orderId}/return takes an optional refund block:
 *
 *	paymentMode  cash | card | original
 *	cardType     stripe | manual            (card only)
 *	refundType   full | partial              (default full)
 *	amountCents  partial only
 *
 * full refunds what the return is worth (its refundCents: gross − discount +
 * tax); partial refunds amountCents of it. Only stripe moves money
 * through a provider — to the Stripe payments on the order, across
 * several when a split was paid with more than one card. Cash and a manual
 * card are handed back at the counter and simply recorded with the return.
 *
 * original refunds the way the order was paid, which is what a split paid
 * in cash and by card needs: the card (Stripe) payments are refunded
 * first, largest remaining first and never more than each still holds, and
 * whatever is left is cash handed back at the counter (CashCents).
 *
 * Without the block the return moves no money, exactly as before.
 */

// Refund statuses on a return.
const (
	RefundSucceeded = "succeeded" // the provider settled it
	RefundPending   = "pending"   // accepted; settles by webhook
	RefundManual    = "manual"    // cash or own terminal: handed back at the counter
)

// ReturnRefundResult is the refund a return made.
type ReturnRefundResult struct {
	PaymentMode string
	CardType    string
	RefundType  string
	AmountCents int64
	// The part of AmountCents handed back in cash at the counter (all of it
	// for a cash refund; what the cards could not take for "original").
	CashCents int64
	Status    string
	Outcome   *paymentsapi.RefundOutcome // provider refunds only
}

// throughProvider: the refund goes back through Stripe.
func throughProvider(p *ordersapi.ReturnRefund) bool {
	return p.PaymentMode == ordersapi.RefundModeCard && p.CardType == ordersapi.CardTypeStripe
}

// normalizeReturnRefund checks the refund block and fills its defaults.
func normalizeReturnRefund(in *ordersapi.ReturnRefund) (*ordersapi.ReturnRefund, error) {
	if in == nil {
		return nil, nil
	}
	p := *in
	p.PaymentMode = strings.ToLower(strings.TrimSpace(p.PaymentMode))
	p.CardType = strings.ToLower(strings.TrimSpace(p.CardType))
	p.RefundType = strings.ToLower(strings.TrimSpace(p.RefundType))

	switch p.PaymentMode {
	case ordersapi.RefundModeCash, ordersapi.RefundModeOriginal:
		p.CardType = ""
	case ordersapi.RefundModeCard:
		switch p.CardType {
		case ordersapi.CardTypeStripe, ordersapi.CardTypeManual:
		default:
			return nil, refundField("refund.cardType", "cardType must be stripe or manual for a card refund.")
		}
	default:
		return nil, refundField("refund.paymentMode", "paymentMode must be cash, card or original.")
	}

	switch p.RefundType {
	case "", ordersapi.RefundTypeFull:
		p.RefundType, p.AmountCents = ordersapi.RefundTypeFull, 0
	case ordersapi.RefundTypePart:
		if p.AmountCents <= 0 {
			return nil, refundField("refund.amountCents", "A partial refund needs amountCents greater than zero.")
		}
	default:
		return nil, refundField("refund.refundType", "refundType must be full or partial.")
	}
	return &p, nil
}

func refundField(field, msg string) error {
	return apperr.Validation(msg).WithFields(apperr.FieldError{Field: field, Code: "INVALID", Message: msg})
}

// refundReturn works out the amount from the priced return and, for Stripe,
// refunds it at the provider. Runs inside the return's transaction:
// its error rolls the return back.
func (s *Service) refundReturn(ctx context.Context, order *ordersapi.Order, plan *ordersapi.ReturnRefund, charges ordersapi.ReturnCharges, reason, issuedBy string, out *ReturnRefundResult) error {
	amount := charges.RefundCents
	if plan.RefundType == ordersapi.RefundTypePart {
		if plan.AmountCents > charges.RefundCents {
			return refundField("refund.amountCents",
				"A partial refund cannot be more than the return is worth ("+money.Format(charges.RefundCents, order.Currency)+").")
		}
		amount = plan.AmountCents
	}
	out.AmountCents = amount
	if reason == "" {
		reason = "Return"
	}
	if plan.PaymentMode == ordersapi.RefundModeOriginal {
		return s.refundToOriginal(ctx, order, amount, reason, issuedBy, out)
	}
	if !throughProvider(plan) || amount <= 0 {
		out.Status = RefundManual
		if plan.PaymentMode == ordersapi.RefundModeCash {
			out.CashCents = amount
		}
		return nil
	}
	outcome, err := s.refundToCards(ctx, order, plan.CardType, amount, true, reason, issuedBy)
	if err != nil {
		return err
	}
	out.Outcome = outcome
	out.Status = RefundSucceeded
	if outcome != nil && outcome.Pending() {
		out.Status = RefundPending
	}
	return nil
}

// refundToOriginal refunds a return the way the order was paid: to its
// Stripe payments as far as they go (capped at what each still
// holds), the rest as cash at the counter.
func (s *Service) refundToOriginal(ctx context.Context, order *ordersapi.Order, amount int64, reason, issuedBy string, out *ReturnRefundResult) error {
	if amount <= 0 {
		out.Status = RefundManual
		return nil
	}
	outcome, err := s.refundToCards(ctx, order, "", amount, false, reason, issuedBy)
	if err != nil {
		return err
	}
	out.Outcome = outcome
	out.CashCents = amount - refundedCents(outcome)
	switch {
	case outcome == nil:
		out.Status = RefundManual
	case outcome.Pending():
		out.Status = RefundPending
	default:
		out.Status = RefundSucceeded
	}
	return nil
}
