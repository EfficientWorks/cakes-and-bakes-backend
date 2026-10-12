package domain

import (
	"bakehouse/pkg/realtime"
	"context"
	"errors"
	"math"
	"sort"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/tenant"
	"bakehouse/services/orders/ordersapi"
)

/*
 * Handing goods back.
 *
 * Either the whole order or particular lines. The quantity comes off the line
 * and the order's totals are recomputed, so every report that reads the order
 * sees what was actually kept. Money is handed back at the counter and
 * reconciled against the return record — there is no card to credit yet.
 */
func (s *Service) ReturnPOSOrder(ctx context.Context, claims *auth.StaffClaims, orderID string, req ordersapi.ReturnRequest) (string, ordersapi.ReturnCharges, *ReturnRefundResult, error) {
	var none ordersapi.ReturnCharges
	if !tenant.HasPermission(claims, tenant.PermOrdersRefundLimited) {
		return "", none, nil, apperr.PermissionDenied("You do not have permission to process returns.")
	}
	order, err := s.GetOrderAdmin(ctx, claims, orderID)
	if err != nil {
		return "", none, nil, err
	}
	if !req.FullReturn && len(req.Items) == 0 {
		return "", none, nil, apperr.Validation("Choose what is being returned.")
	}
	// A website order can come back only once it has been handed over.
	if order.Channel == "web" && order.Status != ordersapi.StatusFulfilled && !ordersapi.KOTHandedOff(order.KOTStatus) {
		return "", none, nil, apperr.New("ORDER_NOT_FULFILLED", apperr.CategoryBusinessRule,
			"This order has not been fulfilled yet, so nothing can be returned.")
	}
	for _, line := range req.Items {
		if line.Quantity <= 0 {
			return "", none, nil, apperr.Validation("A returned quantity must be at least one.")
		}
	}
	if err := validateReturnCharges(req); err != nil {
		return "", none, nil, err
	}
	plan, err := normalizeReturnRefund(req.Refund)
	if err != nil {
		return "", none, nil, err
	}

	var staffID *string
	if claims != nil && claims.StaffID != "" {
		staffID = &claims.StaffID
	}

	// The rate to refund tax at: the order's or the store's, else the rate
	// the order's own tax amount works out to, so tax is refunded either way.
	taxRate := s.orderTaxPct(ctx, order)
	if taxRate == nil {
		if base := order.SubtotalCents - order.DiscountCents; order.TaxCents > 0 && base > 0 {
			r := float64(order.TaxCents) * 100 / float64(base)
			taxRate = &r
		}
	}
	var orderDiscountShare int64
	pricer := returnPricer(req, order, taxRate, &orderDiscountShare)
	// With a card refund through Stripe, the refund is made before
	// the return commits: a refund the provider refuses leaves no return.
	var refund *ReturnRefundResult
	var refundFirst func(ordersapi.ReturnCharges) error
	if plan != nil {
		refund = &ReturnRefundResult{PaymentMode: plan.PaymentMode, CardType: plan.CardType, RefundType: plan.RefundType}
		refundFirst = func(charges ordersapi.ReturnCharges) error {
			return s.refundReturn(ctx, order, plan, charges, req.Reason, derefStr(staffID), refund)
		}
	}
	returnID, charges, err := s.store.RecordReturn(ctx, orderID, req, staffID, pricer, refundFirst)
	if err != nil {
		var ae *apperr.Error
		if errors.As(err, &ae) {
			return "", none, nil, err
		}
		return "", none, nil, apperr.Validation("That return could not be recorded — check the quantities against what is still outstanding.")
	}
	if refund != nil && refund.Outcome != nil {
		_ = s.store.UpdatePaymentStatus(ctx, orderID, paymentStatusAfterRefund(refund.Outcome))
	}

	// The order is now worth less than it was. Recomputed from the lines so
	// every reader agrees, rather than subtracting from a stored total.
	if err := s.recalculateAfterReturn(ctx, orderID, orderDiscountShare, taxRate); err != nil {
		return "", none, nil, err
	}
	_ = s.store.SetUpdatedBiller(ctx, orderID, staffID)

	audit := map[string]any{"returnId": returnID, "amountCents": charges.GrossCents, "full": req.FullReturn,
		"discountCents": charges.DiscountCents, "taxCents": charges.TaxCents, "refundCents": charges.RefundCents}
	if refund != nil {
		audit["refund"] = map[string]any{"paymentMode": refund.PaymentMode, "cardType": refund.CardType,
			"refundType": refund.RefundType, "amountCents": refund.AmountCents, "status": refund.Status}
	}
	s.writeOrderAudit(ctx, claims, "pos.order.return", orderID, audit)
	s.publishOrderByID(ctx, realtime.EventOrderUpdated, orderID)
	// A refund the provider settled at once is final now; a pending one is
	// announced when its webhook arrives.
	if refund != nil && refund.Outcome != nil && refund.Status == RefundSucceeded {
		s.publishPaymentEvent(ctx, EventRefundCompleted, orderID, map[string]any{
			"provider": refund.CardType, "amountCents": refund.AmountCents, "returnId": returnID,
		})
	}

	return returnID, charges, refund, nil
}

// validateReturnCharges checks the discount and tax the till sent on a return.
func validateReturnCharges(req ordersapi.ReturnRequest) error {
	if req.DiscountPct != nil && (*req.DiscountPct < 0 || *req.DiscountPct > 100) {
		return apperr.Validation("discountPct must be between 0 and 100.").
			WithFields(apperr.FieldError{Field: "discountPct", Code: "INVALID", Message: "0 to 100."})
	}
	if req.DiscountCents != nil && *req.DiscountCents < 0 {
		return apperr.Validation("discountCents cannot be negative.").
			WithFields(apperr.FieldError{Field: "discountCents", Code: "INVALID", Message: "Cannot be negative."})
	}
	if req.TaxPct != nil && (*req.TaxPct < 0 || *req.TaxPct > 100) {
		return apperr.Validation("taxPct must be between 0 and 100.").
			WithFields(apperr.FieldError{Field: "taxPct", Code: "INVALID", Message: "0 to 100."})
	}
	if req.TaxCents != nil && *req.TaxCents < 0 {
		return apperr.Validation("taxCents cannot be negative.").
			WithFields(apperr.FieldError{Field: "taxCents", Code: "INVALID", Message: "Cannot be negative."})
	}
	return nil
}

/*
 * returnPricer prices a return line by line.
 *
 * Each returned line on its own:
 *
 *	gross    = unit price × quantity returned
 *	discount = the line's own discount, prorated by the quantity returned
 *	           (a line of 4 with $2 off returns $0.50 per unit), plus the
 *	           line's share of the order discount (the order's rate, or a
 *	           fixed order discount shared by value) on what is left
 *	tax      = the tax rate on gross − discount
 *	refund   = gross − discount + tax
 *
 * What the till states for the return wins: a discount or tax percentage is
 * applied to every line; a cents amount is shared across the lines by value
 * (largest remainder, so the shares add up exactly). The return's totals are
 * the sums of its lines, so item, return and order figures always agree.
 */
//
// orderShareOut receives the part of a fixed order discount that went back
// with these goods, so the order keeps only the rest (recalculateAfterReturn).
func returnPricer(req ordersapi.ReturnRequest, order *ordersapi.Order, orderTaxPct *float64, orderShareOut *int64) ordersapi.ReturnPricer {
	return func(lines []ordersapi.ReturnLineInput) ([]ordersapi.ReturnLineCharges, ordersapi.ReturnCharges) {
		n := len(lines)
		gross := make([]int64, n)
		for i, l := range lines {
			gross[i] = l.UnitPriceCents * int64(l.Quantity)
		}

		// The fixed order discount's share for these goods, from what the
		// order is worth now (both the subtotal and the discount shrink with
		// each return, so the ratio holds across repeated returns).
		orderShare := make([]int64, n)
		if (order.DiscountPct == nil || *order.DiscountPct <= 0) && order.DiscountCents > 0 && order.SubtotalCents > 0 {
			nets := make([]int64, n)
			for i, l := range lines {
				nets[i] = clampNonNegative(gross[i] - min(proratedLineDiscount(l), gross[i]))
			}
			var netSum int64
			for _, v := range nets {
				netSum += v
			}
			share := int64(math.Round(float64(order.DiscountCents) * float64(netSum) / float64(order.SubtotalCents)))
			if share > order.DiscountCents {
				share = order.DiscountCents
			}
			orderShare = shareByWeight(share, nets)
			if orderShareOut != nil {
				*orderShareOut = share
			}
		}

		// Discount per line.
		disc := make([]int64, n)
		switch {
		case req.DiscountCents != nil:
			disc = shareByWeight(*req.DiscountCents, gross)
		case req.DiscountPct != nil:
			for i := range lines {
				disc[i] = int64(math.Round(float64(gross[i]) * *req.DiscountPct / 100))
			}
		default:
			for i, l := range lines {
				own := proratedLineDiscount(l)
				if own > gross[i] {
					own = gross[i]
				}
				net := gross[i] - own
				share := orderShare[i]
				if order.DiscountPct != nil && *order.DiscountPct > 0 {
					share = int64(math.Round(float64(net) * *order.DiscountPct / 100))
				}
				disc[i] = own + share
			}
		}
		for i := range disc {
			if disc[i] > gross[i] {
				disc[i] = gross[i]
			}
			if disc[i] < 0 {
				disc[i] = 0
			}
		}

		// Tax per line, on what is left after its discount.
		taxable := make([]int64, n)
		for i := range lines {
			taxable[i] = gross[i] - disc[i]
		}
		rate := req.TaxPct
		if rate == nil {
			rate = orderTaxPct
		}
		tax := make([]int64, n)
		switch {
		case req.TaxCents != nil:
			tax = shareByWeight(*req.TaxCents, taxable)
		case rate != nil && *rate > 0:
			for i := range lines {
				tax[i] = taxOn(taxable[i], rate)
			}
		}

		out := make([]ordersapi.ReturnLineCharges, n)
		var total ordersapi.ReturnCharges
		for i, l := range lines {
			c := ordersapi.ReturnCharges{GrossCents: gross[i], DiscountCents: disc[i], TaxCents: tax[i]}
			c.DiscountPct = req.DiscountPct
			if c.DiscountPct == nil {
				c.DiscountPct = pctOf(disc[i], gross[i], 100)
			}
			c.TaxPct = taxRateOf(req, rate, tax[i], taxable[i])
			c.RefundCents = clampNonNegative(gross[i] - disc[i] + tax[i])
			out[i] = ordersapi.ReturnLineCharges{OrderItemID: l.OrderItemID, Quantity: l.Quantity, ReturnCharges: c}

			total.GrossCents += c.GrossCents
			total.DiscountCents += c.DiscountCents
			total.TaxCents += c.TaxCents
			total.RefundCents += c.RefundCents
		}
		total.DiscountPct = req.DiscountPct
		if total.DiscountPct == nil {
			total.DiscountPct = pctOf(total.DiscountCents, total.GrossCents, 100)
		}
		total.TaxPct = taxRateOf(req, rate, total.TaxCents, total.GrossCents-total.DiscountCents)
		return out, total
	}
}

// proratedLineDiscount is the part of a line's own discount that belongs to
// the units coming back.
func proratedLineDiscount(l ordersapi.ReturnLineInput) int64 {
	if l.LineDiscountCents > 0 && l.LineQuantity > 0 {
		return int64(math.Round(float64(l.LineDiscountCents) * float64(l.Quantity) / float64(l.LineQuantity)))
	}
	if l.LineDiscountPct != nil && *l.LineDiscountPct > 0 {
		return int64(math.Round(float64(l.UnitPriceCents*int64(l.Quantity)) * *l.LineDiscountPct / 100))
	}
	return 0
}

// taxRateOf is the rate to report: the one applied, or, when the till gave a
// tax amount, what that amount comes to on the taxable value.
func taxRateOf(req ordersapi.ReturnRequest, rate *float64, tax, taxable int64) *float64 {
	if req.TaxCents != nil {
		if req.TaxPct != nil {
			return req.TaxPct
		}
		return pctOf(tax, taxable, 1000)
	}
	if rate != nil && *rate > 0 {
		return rate
	}
	return nil
}

// pctOf is part/whole as a percentage rounded to 1/scale of a percent, or nil
// when there is nothing to report.
func pctOf(part, whole int64, scale float64) *float64 {
	if part <= 0 || whole <= 0 {
		return nil
	}
	p := math.Round(float64(part)*100*scale/float64(whole)) / scale
	return &p
}

func clampNonNegative(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}

// shareByWeight splits total across the weights in proportion, by largest
// remainder, so the shares add up to exactly total.
func shareByWeight(total int64, weights []int64) []int64 {
	out := make([]int64, len(weights))
	var sum int64
	for _, w := range weights {
		if w > 0 {
			sum += w
		}
	}
	if total <= 0 || sum == 0 {
		return out
	}
	type rem struct {
		i int
		r float64
	}
	var assigned int64
	rems := make([]rem, 0, len(weights))
	for i, w := range weights {
		if w <= 0 {
			continue
		}
		exact := float64(total) * float64(w) / float64(sum)
		out[i] = int64(math.Floor(exact))
		assigned += out[i]
		rems = append(rems, rem{i, exact - math.Floor(exact)})
	}
	sort.Slice(rems, func(a, b int) bool { return rems[a].r > rems[b].r })
	for k := 0; assigned < total && k < len(rems); k++ {
		out[rems[k].i]++
		assigned++
	}
	return out
}

// recalculateAfterReturn recomputes the order after a return. A fixed order
// discount keeps only what was not refunded with the goods; tax follows the
// rate the return was priced at.
func (s *Service) recalculateAfterReturn(ctx context.Context, orderID string, orderDiscountShare int64, taxPct *float64) error {
	order, err := s.store.GetOrderByID(ctx, orderID)
	if err != nil {
		return apperr.Internal(err)
	}
	discount := order.DiscountCents
	if order.DiscountPct == nil || *order.DiscountPct <= 0 {
		discount = clampNonNegative(discount - orderDiscountShare)
	}
	return s.recalculatePOSTotals(ctx, orderID, ordersapi.OrderTotals{
		DiscountPct:        order.DiscountPct,
		DiscountCents:      discount,
		TaxCents:           order.TaxCents,
		TaxPct:             taxPct,
		DeliveryFeeCents:   order.DeliveryFeeCents,
		ServiceChargeCents: order.ServiceChargeCents,
		TipCents:           order.TipCents,
	})
}
