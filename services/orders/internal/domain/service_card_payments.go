package domain

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/email"
	"bakehouse/pkg/money"
	"bakehouse/pkg/realtime"
	"bakehouse/pkg/tenant"
	"bakehouse/services/orders/internal/store"
	"bakehouse/services/orders/ordersapi"
	"bakehouse/services/payments/paymentsapi"
)

/*
 * Card payments through Stripe, as orders sees them.
 *
 *   - which providers a store offers: switched on for the store in admin AND
 *     configured on the server (cardProvidersFor);
 *   - payment links: a hosted page emailed to a counter customer, one open per
 *     order (SendPaymentLink / CancelPaymentLink);
 *   - what a provider webhook reports: a link paid becomes a bill payment and
 *     settles the order like any other payment (HandlePaymentEvent);
 *   - refunds back to the card that paid (refundToCards), shared by returns,
 *     cancellations and admin refunds.
 *
 * Cash and a card run on the store's own terminal ("manual") never reach a
 * provider; they are recorded by the till as before.
 */

// Payment events on the live feeds (POS, KDS, admin, the customer's site).
const (
	EventPaymentLinkSent      = "payment.link_sent"
	EventPaymentLinkCancelled = "payment.link_cancelled"
	EventPaymentLinkExpired   = paymentsapi.EventPaymentLinkExpired
	EventPaymentCompleted     = paymentsapi.EventPaymentCompleted
	EventRefundCompleted      = paymentsapi.EventRefundCompleted
	EventRefundFailed         = paymentsapi.EventRefundFailed
)

// UseCardPayments plugs in the payments module's card seam.
func (s *Service) UseCardPayments(cards paymentsapi.Cards) { s.cards = cards }

// cardProvidersFor is what a store can take card payment through right now.
func (s *Service) cardProvidersFor(ctx context.Context, branchID string) ([]string, error) {
	if s.cards == nil {
		return []string{}, nil
	}
	branch, err := s.branches.GetBranch(ctx, branchID)
	if err != nil {
		return nil, apperr.NotFound("Store not found.")
	}
	configured := map[string]bool{}
	for _, p := range s.cards.Providers() {
		configured[p] = true
	}
	out := []string{}
	for _, p := range branch.CardProviders {
		if configured[p] {
			out = append(out, p)
		}
	}
	return out, nil
}

// --- Payment options -------------------------------------------------------------

// PaymentOptions is what the till may offer for an order at a store.
type PaymentOptions struct {
	BranchID      string
	Cash          bool
	ManualCard    bool     // a card on the store's own terminal
	CardProviders []string // payment links / online card
}

// PaymentOptions reads the options for the staff member's store, or for
// branchID when given (and accessible).
func (s *Service) PaymentOptions(ctx context.Context, claims *auth.StaffClaims, branchID string) (*PaymentOptions, error) {
	if branchID == "" && claims != nil {
		branchID = claims.LocationBranchID
	}
	if branchID == "" {
		return nil, apperr.Validation("branchId is required.")
	}
	if !tenant.CanAccessBranch(claims, branchID) {
		return nil, apperr.PermissionDenied("You do not have access to that store.")
	}
	providers, err := s.cardProvidersFor(ctx, branchID)
	if err != nil {
		return nil, err
	}
	// Cash follows the store's "Accept cash on delivery" switch in admin.
	branch, err := s.branches.GetBranch(ctx, branchID)
	if err != nil {
		return nil, apperr.NotFound("Store not found.")
	}
	return &PaymentOptions{BranchID: branchID, Cash: branch.AcceptsCashOnDelivery, ManualCard: true, CardProviders: providers}, nil
}

// --- Payment links ---------------------------------------------------------------

// PaymentLinkRequest asks for a link to be sent. All optional: the provider
// defaults to the store's only one, the amount to what is still owed (on the
// bill when billId is given), the email to the order's purchaser.
type PaymentLinkRequest struct {
	Provider    string
	BillID      *string
	AmountCents int64
	Email       string
	// The part of AmountCents that is tip and service charge (see
	// applyLinkCharges).
	TipCents           int64
	ServiceChargeCents int64
}

// PaymentLinkResult is the link and what happened to send it.
type PaymentLinkResult struct {
	Link     *paymentsapi.PaymentLink
	Resent   bool // the same open link was sent again
	Replaced bool // an older open link was expired first
	// Whether the email reached the mail service; the link works either way
	// and can be shared by hand when it did not.
	EmailSent  bool
	EmailError string
}

// refreshedOpenLink is the order's open link after asking its provider; nil
// when there is none or it turned out to be paid or expired.
func (s *Service) refreshedOpenLink(ctx context.Context, orderID string) (*paymentsapi.PaymentLink, error) {
	open, err := s.cards.OpenLink(ctx, orderID)
	if err != nil || open == nil {
		return open, err
	}
	link, err := s.cards.RefreshLink(ctx, open.ID)
	if err != nil {
		return nil, err
	}
	if link.Status != paymentsapi.LinkOpen {
		return nil, nil
	}
	return link, nil
}

// SendPaymentLink creates (or re-sends) the order's payment link and emails it.
func (s *Service) SendPaymentLink(ctx context.Context, claims *auth.StaffClaims, orderID string, req PaymentLinkRequest) (*PaymentLinkResult, error) {
	if !tenant.HasPermission(claims, tenant.PermOrdersAmend) {
		return nil, apperr.PermissionDenied("You do not have permission to take payment.")
	}
	if s.cards == nil {
		return nil, apperr.New("PAYMENTS_NOT_CONFIGURED", apperr.CategoryBusinessRule, "Card payments are not set up on this server.")
	}
	order, err := s.GetOrderAdmin(ctx, claims, orderID)
	if err != nil {
		return nil, err
	}
	switch order.Status {
	case ordersapi.StatusCancelled, ordersapi.StatusReturned, ordersapi.StatusRefunded:
		return nil, apperr.New("ORDER_CLOSED", apperr.CategoryBusinessRule, "This order is "+order.Status+"; there is nothing to pay.")
	}

	provider, err := s.pickProvider(ctx, order.BranchID, req.Provider)
	if err != nil {
		return nil, err
	}
	billID := req.BillID
	if billID != nil && strings.TrimSpace(*billID) == "" {
		billID = nil
	}
	// An open link may have been paid with its webhook not in yet: settle
	// that first, so a paid order is not sent a second link.
	open, err := s.refreshedOpenLink(ctx, order.ID)
	if err != nil {
		return nil, err
	}
	if open == nil {
		if order, err = s.GetOrderAdmin(ctx, claims, orderID); err != nil {
			return nil, err
		}
	}
	owed, err := s.outstandingCents(ctx, order, billID)
	if err != nil {
		return nil, err
	}
	if owed <= 0 {
		return nil, apperr.New("NOTHING_OWED", apperr.CategoryBusinessRule, "Nothing is left to pay on this order.")
	}
	if req.TipCents < 0 || req.ServiceChargeCents < 0 {
		return nil, apperr.Validation("A tip or service charge cannot be negative.")
	}
	// A tip or service charge chosen on the payment page is part of the
	// amount: it is added to the order first, so it is owed (and the link is
	// not refused as more than is owed).
	if req.AmountCents > owed && req.TipCents+req.ServiceChargeCents > 0 {
		if owed, err = s.applyLinkCharges(ctx, order, billID, req, owed); err != nil {
			return nil, err
		}
	}
	amount := owed
	if req.AmountCents > 0 {
		if req.AmountCents > owed {
			return nil, apperr.Validation("amountCents is more than is owed (" + money.Format(owed, order.Currency) + ").").
				WithFields(apperr.FieldError{Field: "amountCents", Code: "TOO_HIGH", Message: "At most " + money.Format(owed, order.Currency) + "."})
		}
		amount = req.AmountCents
	}
	to := strings.TrimSpace(req.Email)
	if to == "" {
		to = noticeEmailForOrder(order)
	}
	if to == "" || !strings.Contains(to, "@") {
		return nil, apperr.Validation("An email address is needed to send the payment link.").
			WithFields(apperr.FieldError{Field: "email", Code: "REQUIRED", Message: "The customer's email."})
	}

	out := &PaymentLinkResult{}
	if open != nil {
		// The same link asked for again: send it again rather than opening a
		// second page for the same money.
		if open.Provider == provider && open.AmountCents == amount && sameBill(open.BillID, billID) &&
			strings.EqualFold(open.CustomerEmail, to) {
			link, err := s.cards.MarkLinkResent(ctx, open.ID)
			if err != nil {
				return nil, err
			}
			out.Link, out.Resent = link, true
		}
	}
	if out.Link == nil {
		// Anything changed: a new page replaces the old one, which the
		// provider expires so only one link can ever be paid.
		in := paymentsapi.CreateLinkInput{
			OrderID: order.ID, BranchID: order.BranchID, BillID: billID, Provider: provider,
			AmountCents: amount, Currency: order.Currency, CustomerEmail: to,
			Reference: order.Reference, CreatedBy: derefStr(staffIDPtr(claims)),
		}
		if open != nil {
			in.ReplaceLinkID = open.ID
		}
		link, err := s.cards.CreateLink(ctx, in)
		if err != nil {
			return nil, err
		}
		out.Link, out.Replaced = link, open != nil
	}

	if err := s.emailPaymentLink(ctx, order, out.Link); err != nil {
		out.EmailError = "The payment link was created but the email could not be sent. Share the link with the customer another way."
	} else {
		out.EmailSent = s.emailer != nil
	}
	s.writeOrderAudit(ctx, claims, "pos.payment.link", order.ID, map[string]any{
		"linkId": out.Link.ID, "provider": provider, "amountCents": amount, "email": to,
		"resent": out.Resent, "replaced": out.Replaced,
	})
	s.publishPaymentEvent(ctx, EventPaymentLinkSent, order.ID, linkEventData(out.Link, nil))
	return out, nil
}

// GetPaymentLink is the order's latest link, or a NOT_FOUND.
func (s *Service) GetPaymentLink(ctx context.Context, claims *auth.StaffClaims, orderID string) (*paymentsapi.PaymentLink, error) {
	order, err := s.GetOrderAdmin(ctx, claims, orderID)
	if err != nil {
		return nil, err
	}
	if s.cards == nil {
		return nil, apperr.NotFound("This order has no payment link.")
	}
	link, err := s.cards.LatestLink(ctx, order.ID)
	if err != nil {
		return nil, err
	}
	// Polled by the till while it waits: ask the provider too, so a paid
	// link shows as paid even when its webhook is late or missing.
	if link != nil && link.Status == paymentsapi.LinkOpen {
		if link, err = s.cards.RefreshLink(ctx, link.ID); err != nil {
			return nil, err
		}
	}
	if link == nil {
		return nil, apperr.NotFound("This order has no payment link.")
	}
	return link, nil
}

// CancelPaymentLink expires the order's open link so it can no longer be paid.
func (s *Service) CancelPaymentLink(ctx context.Context, claims *auth.StaffClaims, orderID string) (*paymentsapi.PaymentLink, error) {
	if !tenant.HasPermission(claims, tenant.PermOrdersAmend) {
		return nil, apperr.PermissionDenied("You do not have permission to change payments.")
	}
	order, err := s.GetOrderAdmin(ctx, claims, orderID)
	if err != nil {
		return nil, err
	}
	link, err := s.cancelOpenLink(ctx, order.ID)
	if err != nil {
		return nil, err
	}
	if link == nil {
		return nil, apperr.New("NO_OPEN_LINK", apperr.CategoryBusinessRule, "This order has no open payment link.")
	}
	s.writeOrderAudit(ctx, claims, "pos.payment.link.cancel", order.ID, map[string]any{"linkId": link.ID})
	return link, nil
}

// cancelOpenLink expires the order's open link, if any, and says so on the
// live feeds. Used by CancelPaymentLink and when an order is cancelled.
func (s *Service) cancelOpenLink(ctx context.Context, orderID string) (*paymentsapi.PaymentLink, error) {
	if s.cards == nil {
		return nil, nil
	}
	open, err := s.cards.OpenLink(ctx, orderID)
	if err != nil || open == nil {
		return nil, err
	}
	if err := s.cards.CancelLink(ctx, open.ID); err != nil {
		return nil, err
	}
	open.Status = paymentsapi.LinkCancelled
	s.publishPaymentEvent(ctx, EventPaymentLinkCancelled, orderID, linkEventData(open, nil))
	return open, nil
}

func (s *Service) pickProvider(ctx context.Context, branchID, asked string) (string, error) {
	providers, err := s.cardProvidersFor(ctx, branchID)
	if err != nil {
		return "", err
	}
	asked = strings.ToLower(strings.TrimSpace(asked))
	if asked == "" {
		if len(providers) == 1 {
			return providers[0], nil
		}
		if len(providers) == 0 {
			return "", errNoCardProvider()
		}
		return "", apperr.Validation("Choose a card provider for this store.").
			WithFields(apperr.FieldError{Field: "provider", Code: "REQUIRED", Message: strings.Join(providers, " or ")})
	}
	for _, p := range providers {
		if p == asked {
			return p, nil
		}
	}
	return "", apperr.New("PROVIDER_NOT_ENABLED", apperr.CategoryBusinessRule,
		"This store does not take card payments through "+asked+". Switch it on for the store in admin.").
		WithMeta(map[string]any{"cardProviders": providers})
}

func errNoCardProvider() error {
	return apperr.New("PROVIDER_NOT_ENABLED", apperr.CategoryBusinessRule,
		"This store has no online card provider switched on. Turn on Stripe for it in admin.")
}

// outstandingCents is what is still owed on a bill, or on the whole order.
func (s *Service) outstandingCents(ctx context.Context, order *ordersapi.Order, billID *string) (int64, error) {
	if billID != nil {
		bills, err := s.store.ListBills(ctx, order.ID)
		if err != nil {
			return 0, apperr.Internal(err)
		}
		for _, b := range bills {
			if b.ID == *billID {
				return b.AmountCents - b.PaidCents, nil
			}
		}
		return 0, apperr.NotFound("That bill is not part of this order.")
	}
	payments, err := s.store.ListBillPayments(ctx, order.ID)
	if err != nil {
		return 0, apperr.Internal(err)
	}
	taken := int64(0)
	for _, p := range payments {
		taken += p.AmountCents + p.TipCents
	}
	return order.TotalCents - taken, nil
}

/*
 * applyLinkCharges adds the tip and service charge of a payment link to the
 * order, the way a cash payment's tip is added (RecordBillPayment).
 *
 * Only the part of the link above what is owed is added, so sending the
 * same link again adds nothing, and changing the tip on a link that
 * replaces an unpaid one adds only the difference. The tip is counted first,
 * then the service charge. On a split bill the bill grows by the same
 * amount, so the share and the order stay in step. Returns the new amount
 * owed.
 */
func (s *Service) applyLinkCharges(ctx context.Context, order *ordersapi.Order, billID *string, req PaymentLinkRequest, owed int64) (int64, error) {
	extra := req.AmountCents - owed
	if extra <= 0 {
		return owed, nil
	}
	if limit := req.TipCents + req.ServiceChargeCents; extra > limit {
		return owed, apperr.Validation("amountCents is more than is owed (" + money.Format(owed, order.Currency) + ") plus the tip and service charge.").
			WithFields(apperr.FieldError{Field: "amountCents", Code: "TOO_HIGH", Message: "At most " + money.Format(owed+limit, order.Currency) + "."})
	}
	// A service charge already on the order (set when it was split, say) is
	// not added again; only what the bill screen adds beyond it.
	service := min(max(req.ServiceChargeCents-order.ServiceChargeCents, 0), extra)
	tip := min(req.TipCents, extra-service)
	if tip+service < extra {
		return owed, apperr.Validation("amountCents is more than is owed (" + money.Format(owed, order.Currency) + ") plus the tip and service charge.").
			WithFields(apperr.FieldError{Field: "amountCents", Code: "TOO_HIGH", Message: "At most " + money.Format(owed+tip+service, order.Currency) + "."})
	}
	if err := s.store.AddOrderCharges(ctx, order.ID, billID, tip, service); err != nil {
		return owed, apperr.Internal(err)
	}
	return owed + extra, nil
}

func sameBill(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// billNoLabel is the bill number as the till shows it ("74", "01"), "" when
// the order has none.
func billNoLabel(o *ordersapi.Order) string {
	if o.BillNo == nil {
		return ""
	}
	return fmt.Sprintf("%02d", *o.BillNo)
}

func (s *Service) emailPaymentLink(ctx context.Context, order *ordersapi.Order, link *paymentsapi.PaymentLink) error {
	if s.emailer == nil {
		return nil
	}
	name := strings.TrimSpace(order.PurchaserFirstName + " " + order.PurchaserLastName)
	if err := s.emailer.Send(ctx, email.Email{
		To:         link.CustomerEmail,
		Template:   email.TemplatePaymentLink,
		BusinessID: businessOf(order),
		Data: map[string]interface{}{
			"reference": order.Reference, "billNo": billNoLabel(order), "customerName": name,
			"url": link.URL, "amountCents": link.AmountCents,
		},
	}); err != nil {
		// The link exists and the till can show it; a failed email is
		// reported, not fatal.
		slog.Error("payment link email failed", "reference", order.Reference, "to", link.CustomerEmail, "cause", err)
		return err
	}
	return nil
}

// --- Provider webhook events -------------------------------------------------------

// HandlePaymentEvent is told by the payments module what a provider webhook
// reported. It records the money and tells the live screens.
func (s *Service) HandlePaymentEvent(ctx context.Context, e paymentsapi.Event) {
	switch e.Type {
	case paymentsapi.EventPaymentCompleted:
		s.recordLinkPayment(ctx, e)
	case paymentsapi.EventPaymentLinkExpired:
		s.publishPaymentEvent(ctx, EventPaymentLinkExpired, e.OrderID, linkEventData(e.Link, nil))
	case paymentsapi.EventRefundCompleted, paymentsapi.EventRefundFailed:
		s.publishPaymentEvent(ctx, e.Type, e.OrderID, map[string]any{
			"provider": e.Provider, "amountCents": e.AmountCents, "refundRef": e.RefundRef, "paymentRef": e.PaymentRef,
		})
	}
}

// recordLinkPayment books a paid link as a card payment on the order (on its
// bill, when the link was for one) and closes the order if that settles it —
// the same path a payment taken at the till goes down.
func (s *Service) recordLinkPayment(ctx context.Context, e paymentsapi.Event) {
	if e.Link == nil {
		return
	}
	order, err := s.store.GetOrderByID(ctx, e.OrderID)
	if err != nil {
		slog.Error("payment link paid for an unknown order", "order", e.OrderID, "payment", e.PaymentRef)
		return
	}
	provider, ref, linkID := e.Provider, e.PaymentRef, e.Link.ID
	p := ordersapi.BillPayment{
		BillID: e.Link.BillID, Method: "card", AmountCents: e.AmountCents, Reference: ref,
		Provider: &provider, ProviderRef: &ref, PaymentLinkID: &linkID,
	}
	if order.Status == ordersapi.StatusCancelled {
		// Paid after the order was cancelled: the money is real, so it is
		// still recorded, and the screens are told so staff can refund it.
		slog.Warn("payment link paid on a cancelled order", "order", order.Reference, "payment", ref)
	}
	settledBill, settledOrder, _, err := s.store.RecordBillPayment(ctx, order.ID, p, nil)
	if err != nil {
		if errors.Is(err, store.ErrDuplicatePayment) {
			return // this provider payment is already on the order
		}
		slog.Error("could not record payment link payment", "order", order.Reference, "payment", ref, "error", err)
		return
	}
	if order.Status != ordersapi.StatusCancelled {
		s.closeIfPaid(ctx, "system", nil, order, settledOrder)
	}
	data := linkEventData(e.Link, map[string]any{
		"billSettled": settledBill, "orderSettled": settledOrder, "orderCancelled": order.Status == ordersapi.StatusCancelled,
	})
	s.publishPaymentEvent(ctx, EventPaymentCompleted, order.ID, data)
	s.writeOrderAudit(ctx, nil, "payment.link.paid", order.ID, data)
}

// publishPaymentEvent sends a payment event about an order to every live
// feed that watches its store (and to its customer's site).
func (s *Service) publishPaymentEvent(ctx context.Context, eventType, orderID string, data map[string]any) {
	o, err := s.store.GetOrderByID(ctx, orderID)
	if err != nil {
		return
	}
	h := s.hub
	if h == nil {
		h = realtime.Default
	}
	e := realtime.Event{
		Type: eventType, OrderID: o.ID, BranchID: o.BranchID, Reference: o.Reference,
		Status: o.Status, KOTStatus: o.KOTStatus, Data: data,
	}
	if e.Data == nil {
		e.Data = map[string]any{}
	}
	e.Data["paymentStatus"] = o.PaymentStatus
	if o.CustomerID != nil {
		e.CustomerID = *o.CustomerID
	}
	h.Publish(e)
	// Screens that only know order events still refresh.
	s.publishOrder(realtime.EventOrderUpdated, o, "")
}

func linkEventData(l *paymentsapi.PaymentLink, extra map[string]any) map[string]any {
	out := map[string]any{}
	if l != nil {
		out["linkId"], out["provider"], out["amountCents"], out["linkStatus"] = l.ID, l.Provider, l.AmountCents, l.Status
		if l.BillID != nil {
			out["billId"] = *l.BillID
		}
		if l.PaymentRef != nil {
			out["paymentRef"] = *l.PaymentRef
		}
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

// --- Refunds to the card that paid ---------------------------------------------------

// knownCharges is the order's own payment id (a website order), so the payments
// ledger can be backfilled for an order placed before it was linked.
func knownCharges(o *ordersapi.Order) []paymentsapi.KnownCharge {
	if o.ProviderPaymentID == nil || paymentsapi.ProviderOf(*o.ProviderPaymentID) == "" {
		return nil
	}
	return []paymentsapi.KnownCharge{{PaymentRef: *o.ProviderPaymentID, AmountCents: o.TotalCents, Currency: o.Currency}}
}

// refundToCards gives amountCents back through the providers that took the
// order's card payments (one provider only when given).
//
// exact: the whole amount must go back to cards, or nothing is refunded (a
// return the till said was paid by card). Not exact: as much as the cards can
// take, the rest being handed back in cash at the counter (cancellations and
// admin refunds of orders paid partly in cash). Returns nil when nothing went
// to a provider.
func (s *Service) refundToCards(ctx context.Context, o *ordersapi.Order, provider string, amountCents int64, exact bool, reason, issuedBy string) (*paymentsapi.RefundOutcome, error) {
	if amountCents <= 0 {
		return nil, nil
	}
	if s.cards == nil {
		if exact {
			return nil, apperr.New("PAYMENTS_NOT_CONFIGURED", apperr.CategoryBusinessRule, "Card payments are not set up on this server.")
		}
		return nil, nil
	}
	known := knownCharges(o)
	if !exact {
		refundable, err := s.cards.RefundableCents(ctx, o.ID, provider, known)
		if err != nil {
			return nil, err
		}
		amountCents = min(amountCents, refundable)
		if amountCents <= 0 {
			return nil, nil
		}
	}
	return s.cards.Refund(ctx, paymentsapi.RefundInput{
		OrderID: o.ID, BranchID: o.BranchID, Provider: provider, AmountCents: amountCents,
		Reason: reason, IssuedBy: issuedBy, Known: known,
	})
}

func refundedCents(o *paymentsapi.RefundOutcome) int64 {
	if o == nil {
		return 0
	}
	return o.RefundedCents
}

// paymentStatusAfterRefund: refunded once nothing is left to give back on the
// order's card payments and the order has no other money, else partial.
func paymentStatusAfterRefund(outcome *paymentsapi.RefundOutcome) string {
	if outcome != nil && outcome.RemainingCents == 0 {
		return ordersapi.PaymentRefunded
	}
	return ordersapi.PaymentPartialRefund
}
