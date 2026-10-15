package domain

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/money"
	"bakehouse/pkg/realtime"
	"bakehouse/pkg/tenant"
	"bakehouse/services/franchise/franchiseapi"
	"bakehouse/services/orders/internal/store"
	"bakehouse/services/orders/ordersapi"
)

// SplitRequest is a bill being cut up at the counter.
type SplitRequest struct {
	Mode string

	// Amount split: how many equal shares, or the exact amounts chosen.
	Ways    int
	Amounts []int64

	// Item split: which lines land on which bill.
	Groups [][]ordersapi.BillItem

	// Custom split: an amount per payment type, which is still just a list of
	// amounts once it reaches here.
	Labels []string

	// The bill screen's service charge, applied to the order before it is
	// split. Nil leaves the order's as it is.
	ServiceChargeCents *int64
}

/*
 * Cutting the bill.
 *
 * The three modes differ only in how the amounts are arrived at. Equal shares
 * are the total divided; a custom split is the amounts typed in; an item split
 * is the lines added up. After that they are the same thing — a list of sums
 * that has to equal what is owed — so they converge here and the settling code
 * never has to know which was used.
 */
func (s *Service) SplitOrder(ctx context.Context, claims *auth.StaffClaims, orderID string, req SplitRequest) ([]ordersapi.Bill, error) {
	if !tenant.HasPermission(claims, tenant.PermOrdersAmend) {
		return nil, apperr.PermissionDenied("You do not have permission to split a bill.")
	}
	order, err := s.GetOrderAdmin(ctx, claims, orderID)
	if err != nil {
		return nil, err
	}
	if order.PaymentStatus == "captured" {
		return nil, apperr.Validation("This order is already paid.")
	}
	// The service charge picked on the bill screen goes onto the order first
	// (and into its total), so the shares add up to what is really owed.
	if sc := req.ServiceChargeCents; sc != nil && *sc != order.ServiceChargeCents {
		if *sc < 0 {
			return nil, apperr.Validation("A service charge cannot be negative.")
		}
		if err := s.setServiceCharge(ctx, order, *sc); err != nil {
			return nil, err
		}
		if order, err = s.GetOrderAdmin(ctx, claims, orderID); err != nil {
			return nil, err
		}
	}

	mode := strings.TrimSpace(req.Mode)
	// "count" is what a till calls the equal split; it is the amount mode.
	if mode == "count" {
		mode = ordersapi.SplitByAmount
	}
	var bills []ordersapi.Bill

	switch mode {
	case ordersapi.SplitByAmount:
		bills, err = splitByAmount(order.TotalCents, order.Currency, req)
	case ordersapi.SplitCustom:
		bills, err = splitCustom(order.TotalCents, order.Currency, req)
	case ordersapi.SplitByItem:
		bills, err = s.splitByItem(ctx, orderID, order.TotalCents, req)
	default:
		return nil, apperr.Validation("A split is by amount, by item, or custom.").
			WithFields(apperr.FieldError{Field: "mode", Code: "INVALID", Message: "amount, item or custom."})
	}
	if err != nil {
		return nil, err
	}

	saved, err := s.store.ReplaceBills(ctx, orderID, mode, bills)
	if err != nil {
		return nil, apperr.New("SPLIT_REFUSED", apperr.CategoryBusinessRule,
			"This bill cannot be re-split — part of it has already been paid.")
	}

	_ = s.store.SetUpdatedBiller(ctx, orderID, staffIDPtr(claims))
	s.writeOrderAudit(ctx, claims, "pos.order.split", orderID,
		map[string]any{"mode": mode, "bills": len(saved)})
	s.publishOrderByID(ctx, realtime.EventOrderUpdated, orderID)
	return saved, nil
}

/*
 * Equal shares.
 *
 * Every share is the total divided and rounded down to the penny, and the
 * pennies that do not divide go on the last share: three ways of £81.40 is
 * 27.13 / 27.13 / 27.14. The shares always add up to the order, so the
 * payments taken against them add up to it too — a split that came to a
 * penny short left an order that could not close without a write-off.
 */
func splitByAmount(total int64, currency string, req SplitRequest) ([]ordersapi.Bill, error) {
	if len(req.Amounts) > 0 {
		return splitCustom(total, currency, req)
	}
	ways := req.Ways
	if ways < 2 {
		return nil, apperr.Validation("A split is between at least two people.").
			WithFields(apperr.FieldError{Field: "ways", Code: "MIN", Message: "At least 2."})
	}
	if ways > 100 {
		return nil, apperr.Validation("That is more ways than a bill can be split.")
	}

	share := total / int64(ways)
	if share <= 0 {
		return nil, apperr.Validation("That bill is too small to split that many ways.")
	}

	remainder := total - share*int64(ways)

	bills := make([]ordersapi.Bill, 0, ways)
	for i := 0; i < ways; i++ {
		amount := share
		if i == ways-1 {
			amount += remainder
		}
		bills = append(bills, ordersapi.Bill{
			AmountCents: amount,
			Label:       fmt.Sprintf("Payment %d of %d", i+1, ways),
		})
	}
	return bills, nil
}

/*
 * Chosen amounts.
 *
 * The last share is whatever is left, so the operator types the amounts they
 * know — "she's paying £20, he's paying £15" — and the rest falls out. They
 * have to add up to the order: a split that comes to less would leave money
 * nobody is being asked for, and one that comes to more would take too much.
 */
func splitCustom(total int64, currency string, req SplitRequest) ([]ordersapi.Bill, error) {
	if len(req.Amounts) == 0 {
		return nil, apperr.Validation("Enter the amounts to split into.")
	}

	var sum int64
	for _, a := range req.Amounts {
		if a <= 0 {
			return nil, apperr.Validation("Each share has to be more than nothing.")
		}
		sum += a
	}
	if sum > total {
		return nil, apperr.Validation(fmt.Sprintf(
			"Those shares come to more than the bill. There is %s to split.", money.Format(total, currency)))
	}

	bills := make([]ordersapi.Bill, 0, len(req.Amounts)+1)
	for i, a := range req.Amounts {
		label := fmt.Sprintf("Payment %d", i+1)
		if i < len(req.Labels) && strings.TrimSpace(req.Labels[i]) != "" {
			label = req.Labels[i]
		}
		bills = append(bills, ordersapi.Bill{AmountCents: a, Label: label})
	}
	// Whatever is left is its own share, so the bills always add up.
	if rest := total - sum; rest > 0 {
		bills = append(bills, ordersapi.Bill{
			AmountCents: rest,
			Label:       fmt.Sprintf("Payment %d", len(bills)+1),
		})
	}
	return bills, nil
}

/*
 * By item.
 *
 * Each group is a set of lines and its bill is what those lines come to. Any
 * line nobody claimed becomes a final bill of its own rather than being
 * silently absorbed — an unclaimed item is a question for the table, not
 * something the till should decide.
 */
func (s *Service) splitByItem(ctx context.Context, orderID string, total int64, req SplitRequest) ([]ordersapi.Bill, error) {
	if len(req.Groups) == 0 {
		return nil, apperr.Validation("Put the items onto bills first.")
	}

	lines, err := s.store.LinesForTotals(ctx, orderID)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	byID := make(map[string]ordersapi.OrderLineAmount, len(lines))
	outstanding := make(map[string]int, len(lines))
	for _, l := range lines {
		byID[l.ID] = l
		outstanding[l.ID] = l.Quantity - l.ReturnedQuantity
	}

	bills := make([]ordersapi.Bill, 0, len(req.Groups)+1)
	var assigned int64
	for i, group := range req.Groups {
		var amount int64
		items := make([]ordersapi.BillItem, 0, len(group))
		for _, it := range group {
			l, known := byID[it.OrderItemID]
			if !known {
				return nil, apperr.Validation("That item is not on this order.").
					WithFields(apperr.FieldError{Field: "groups", Code: "UNKNOWN_ITEM", Message: it.OrderItemID})
			}
			if it.Quantity <= 0 {
				return nil, apperr.Validation("A quantity on a bill has to be at least one.")
			}
			if it.Quantity > outstanding[it.OrderItemID] {
				return nil, apperr.Validation("More of that item has been put on bills than the order holds.")
			}
			outstanding[it.OrderItemID] -= it.Quantity
			// The line's own discount stays with the units on this bill;
			// only order-wide amounts are shared out below.
			line := lineNetCents(l, it.Quantity)
			amount += line
			items = append(items, ordersapi.BillItem{
				OrderItemID: it.OrderItemID,
				Quantity:    it.Quantity,
				AmountCents: line,
			})
		}
		assigned += amount
		bills = append(bills, ordersapi.Bill{
			AmountCents: amount,
			Label:       fmt.Sprintf("Bill %d", i+1),
			Items:       items,
		})
	}

	// Tax, delivery, service charge, tip and any order-level discount — what
	// the order comes to beyond its lines — is shared out across the bills in
	// proportion to the goods on each, so two people with the same items pay
	// the same. It used to ride on the last bill, which left the first
	// bills short of their fair share and the last one owing the lot.
	apportion(bills, total-assigned)
	return bills, nil
}

// lineNetCents is what qty units of a line cost after the line's own
// discount (its cents prorated by quantity, else its percentage).
func lineNetCents(l ordersapi.OrderLineAmount, qty int) int64 {
	gross := l.UnitPriceCents * int64(qty)
	var disc int64
	switch {
	case l.DiscountCents > 0 && l.Quantity > 0:
		disc = int64(math.Round(float64(l.DiscountCents) * float64(qty) / float64(l.Quantity)))
	case l.DiscountPct != nil && *l.DiscountPct > 0:
		disc = int64(math.Round(float64(gross) * *l.DiscountPct / 100))
	}
	if disc > gross {
		disc = gross
	}
	return gross - disc
}

// setServiceCharge puts a service charge on the order and recomputes its
// totals from the lines, as any other charge change does.
func (s *Service) setServiceCharge(ctx context.Context, order *ordersapi.Order, cents int64) error {
	return s.recalculatePOSTotals(ctx, order.ID, ordersapi.OrderTotals{
		DiscountPct:        order.DiscountPct,
		DiscountCents:      order.DiscountCents,
		TaxCents:           order.TaxCents,
		TaxPct:             s.orderTaxPct(ctx, order),
		DeliveryFeeCents:   order.DeliveryFeeCents,
		ServiceChargeCents: cents,
		TipCents:           order.TipCents,
	})
}

/*
 * apportion shares an amount across bills in proportion to what each already
 * carries, to the penny.
 *
 * Each bill gets the floor of its exact share; the pennies that do not divide
 * go, one each, to the bills with the largest fractional share (the
 * largest-remainder method), so the pieces always add up to the amount and no
 * bill is more than a penny from its exact share. A negative amount — an
 * order-level discount bigger than the extras — is shared the same way.
 * With nothing on any bill to weight by, it is split evenly.
 */
func apportion(bills []ordersapi.Bill, amount int64) {
	if amount == 0 || len(bills) == 0 {
		return
	}
	var weightTotal int64
	for _, b := range bills {
		weightTotal += b.AmountCents
	}
	weights := make([]int64, len(bills))
	for i, b := range bills {
		weights[i] = b.AmountCents
	}
	if weightTotal <= 0 {
		for i := range weights {
			weights[i] = 1
		}
		weightTotal = int64(len(bills))
	}

	sign := int64(1)
	if amount < 0 {
		sign, amount = -1, -amount
	}

	shares := make([]int64, len(bills))
	remainders := make([]int64, len(bills))
	var given int64
	for i := range bills {
		exact := amount * weights[i]
		shares[i] = exact / weightTotal
		remainders[i] = exact % weightTotal
		given += shares[i]
	}
	// Hand the leftover pennies to the largest fractional shares first.
	for left := amount - given; left > 0; left-- {
		best := -1
		for i := range bills {
			if best == -1 || remainders[i] > remainders[best] {
				best = i
			}
		}
		shares[best]++
		remainders[best] = -1
	}
	for i := range bills {
		bills[i].AmountCents += sign * shares[i]
	}
}

/*
 * Taking a payment.
 *
 * Against one bill or against the whole order — the till does both, and the
 * only difference here is whether a bill id came with it. Everything else,
 * including deciding whether the order is now settled, happens in one
 * transaction so two tills cannot both conclude they took the last payment.
 */
func (s *Service) TakePayment(ctx context.Context, claims *auth.StaffClaims, orderID string, p ordersapi.BillPayment, serviceChargeCents *int64) (*ordersapi.BillPayment, bool, error) {
	if !tenant.HasPermission(claims, tenant.PermOrdersAmend) {
		return nil, false, apperr.PermissionDenied("You do not have permission to take payment.")
	}
	if p.BillID != nil && strings.TrimSpace(*p.BillID) == "" {
		p.BillID = nil
	}
	order, err := s.GetOrderAdmin(ctx, claims, orderID)
	if err != nil {
		return nil, false, err
	}
	if order.Status == "cancelled" {
		return nil, false, apperr.Validation("This order was cancelled.")
	}
	if p.Method != "cash" && p.Method != "card" {
		return nil, false, apperr.Validation("Payment is by cash or card.").
			WithFields(apperr.FieldError{Field: "method", Code: "INVALID", Message: "cash or card."})
	}
	if p.AmountCents <= 0 {
		return nil, false, apperr.Validation("A payment has to be more than nothing.")
	}
	if p.TipCents < 0 {
		return nil, false, apperr.Validation("A tip cannot be negative.")
	}

	if claims != nil {
		p.TakenBy = staffIDPtr(claims)
	}
	// A card taken at the till is run on the store's own terminal. Stripe and
	// Stripe card payments arrive through payment links instead.
	if p.Method == "card" && p.Provider == nil {
		manual := "manual"
		p.Provider = &manual
	}

	settledBill, settledOrder, change, err := s.store.RecordBillPayment(ctx, orderID, p, serviceChargeCents)
	if err != nil {
		return nil, false, apperr.Internal(err)
	}
	p.ChangeCents = change

	/*
	 * Paid in full means done.
	 *
	 * A counter order is handed over when the money is taken, so the last
	 * payment is what closes it: the status moves to fulfilled here rather
	 * than waiting for a second call the till would otherwise have to make.
	 * Recorded through UpdateOrderStatus so the timeline gets its event and
	 * the fulfilment stamps (deliver date/time, duration) are set the same
	 * way as every other route to fulfilled.
	 */
	if err := s.closeIfPaid(ctx, "staff", staffIDPtr(claims), order, settledOrder); err != nil {
		return nil, false, err
	}

	auditDetails := map[string]any{
		"method": p.Method, "amountCents": p.AmountCents,
		"billSettled": settledBill, "orderSettled": settledOrder,
	}
	if serviceChargeCents != nil {
		auditDetails["serviceChargeCents"] = *serviceChargeCents
	}
	s.writeOrderAudit(ctx, claims, "pos.payment.take", orderID, auditDetails)
	return &p, settledOrder, nil
}

// closeIfPaid closes an order its last payment settled — from the till or
// from a payment link's webhook alike. Paid before the kitchen finished (KDS
// flow), the KDS handing it over closes it instead.
func (s *Service) closeIfPaid(ctx context.Context, actorType string, actorID *string, order *ordersapi.Order, settledOrder bool) error {
	if !settledOrder || order.Status == ordersapi.StatusFulfilled || order.Status == ordersapi.StatusReturned {
		return nil
	}
	if s.kitchenStillWorking(ctx, order) {
		s.publishOrderByID(ctx, realtime.EventOrderUpdated, order.ID)
		return nil
	}
	if _, err := s.store.UpdateOrderStatus(ctx, order.ID, ordersapi.StatusFulfilled, actorType, actorID, "Paid in full"); err != nil {
		return apperr.Internal(err)
	}
	s.publishOrderByID(ctx, realtime.EventOrderStatusChanged, order.ID)
	return nil
}

/*
 * Clearing a split.
 *
 * The counter changed its mind — everyone is paying together after all, or
 * the table wants the lines re-added before it is cut again. The shares go,
 * the order goes back to one bill. Refused once any share has been paid:
 * money that has changed hands is not something a gesture should erase.
 */
func (s *Service) ClearSplit(ctx context.Context, claims *auth.StaffClaims, orderID string) (int64, error) {
	if !tenant.HasPermission(claims, tenant.PermOrdersAmend) {
		return 0, apperr.PermissionDenied("You do not have permission to change a bill split.")
	}
	if _, err := s.GetOrderAdmin(ctx, claims, orderID); err != nil {
		return 0, err
	}
	removed, err := s.store.ClearBills(ctx, orderID)
	if err != nil {
		if errors.Is(err, store.ErrBillsSettled) {
			return 0, apperr.New("SPLIT_REFUSED", apperr.CategoryBusinessRule,
				"This split cannot be cleared — part of it has already been paid.")
		}
		return 0, apperr.Internal(err)
	}
	_ = s.store.SetUpdatedBiller(ctx, orderID, staffIDPtr(claims))
	s.writeOrderAudit(ctx, claims, "pos.order.split.clear", orderID, map[string]any{"bills": removed})
	s.publishOrderByID(ctx, realtime.EventOrderUpdated, orderID)
	return removed, nil
}

// ListBills reads an order's split and what each share has taken.
func (s *Service) ListBills(ctx context.Context, claims *auth.StaffClaims, orderID string) ([]ordersapi.Bill, []ordersapi.BillPayment, error) {
	if _, err := s.GetOrderAdmin(ctx, claims, orderID); err != nil {
		return nil, nil, err
	}
	bills, err := s.store.ListBills(ctx, orderID)
	if err != nil {
		return nil, nil, apperr.Internal(err)
	}
	payments, err := s.store.ListBillPayments(ctx, orderID)
	if err != nil {
		return nil, nil, apperr.Internal(err)
	}
	return bills, payments, nil
}

/*
 * kitchenStillWorking is the KDS fulfilment rule for a POS payment.
 *
 *   send KOT → kitchen ready → payment      payment fulfils (unchanged)
 *   payment → send KOT → kitchen hands over  the KDS fulfils it
 *
 * The second only applies where the store has turned the KDS on
 * (kds_settings.kds_enabled) — a store without a KDS keeps closing orders at
 * the till exactly as before.
 */
func (s *Service) kitchenStillWorking(ctx context.Context, o *ordersapi.Order) bool {
	if s.kds == nil {
		return false
	}
	if o.KOTStatus != ordersapi.KOTQueue && o.KOTStatus != ordersapi.KOTPreparing {
		return false
	}
	st, err := s.kds.KDSSettingsFor(ctx, franchiseapi.KDSScope{Type: franchiseapi.LocationBranch, BranchID: o.BranchID})
	return err == nil && st.KDSEnabled
}
