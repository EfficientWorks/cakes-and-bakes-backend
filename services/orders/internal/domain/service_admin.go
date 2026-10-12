package domain

import (
	"context"
	"log/slog"
	"strings"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/audit"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/money"
	"bakehouse/pkg/realtime"
	"bakehouse/pkg/tenant"
	"bakehouse/pkg/timex"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/orders/internal/store"
	"bakehouse/services/orders/ordersapi"
)

// ListOrdersAdmin applies ADM-01/ADM-02: staff without brand-wide scope
// only ever see their own branch/franchise, enforced here rather than left
// to the caller to remember to filter.
func (s *Service) ListOrdersAdmin(ctx context.Context, claims *auth.StaffClaims, f ordersapi.ListOrdersFilter) ([]ordersapi.Order, int, error) {
	f, err := s.scopeListFilter(ctx, claims, f)
	if err != nil {
		return nil, 0, err
	}
	orders, total, err := s.store.ListOrders(ctx, f)
	if err != nil {
		return nil, 0, apperr.Internal(err)
	}
	return orders, total, nil
}

// SummarizeOrdersAdmin totals the same filtered set ListOrdersAdmin pages
// through — total price and scheduled count are for every matching order,
// not just the page on screen.
func (s *Service) SummarizeOrdersAdmin(ctx context.Context, claims *auth.StaffClaims, f ordersapi.ListOrdersFilter) (ordersapi.OrderListSummary, error) {
	f, err := s.scopeListFilter(ctx, claims, f)
	if err != nil {
		return ordersapi.OrderListSummary{}, err
	}
	sum, err := s.store.SummarizeOrders(ctx, f)
	if err != nil {
		return ordersapi.OrderListSummary{}, apperr.Internal(err)
	}
	return sum, nil
}

// scopeListFilter checks the caller may see what they asked for and narrows
// an unscoped request to what their token holds.
func (s *Service) scopeListFilter(ctx context.Context, claims *auth.StaffClaims, f ordersapi.ListOrdersFilter) (ordersapi.ListOrdersFilter, error) {
	if !tenant.HasPermission(claims, tenant.PermOrdersView) {
		return f, apperr.PermissionDenied("You do not have permission to view orders.")
	}
	switch {
	case f.BranchID != "":
		branch, err := s.branches.GetBranch(ctx, f.BranchID)
		if err != nil {
			return f, apperr.NotFound("Branch not found.")
		}
		if !tenant.CanAccessBranch(claims, "", branch.ID) {
			return f, apperr.PermissionDenied("You do not have access to this branch.")
		}

	case !tenant.IsBrandScoped(claims):
		/*
		 * No scope asked for, and the caller is not brand-wide — so narrow the
		 * query to what they hold rather than refusing it.
		 *
		 * Their scope is in their token; making the console send it back was
		 * asking the client to restate something the server already knows, and
		 * every screen that forgot got a validation error instead of a list.
		 * Narrowing here is also the safer default: a missing filter can now
		 * only ever return less, never the whole network.
		 */
		_, branchID := tenant.OwnScope(claims)
		if branchID == "" {
			return f, apperr.PermissionDenied("Your account has no outlet assigned. Ask an administrator to set one.")
		}
		f.BranchID = branchID
	}
	return f, nil
}

// ResendConfirmationAdmin sends the order confirmation again.
//
// Asked for constantly at a counter: a customer mistypes an address, or the
// first one lands in spam, and somebody needs the email to arrive without
// touching the order. Unlike the send at checkout, a failure here is
// reported — the whole point of pressing the button is to find out whether
// it worked.
func (s *Service) ResendConfirmationAdmin(ctx context.Context, claims *auth.StaffClaims, orderID string) (string, error) {
	o, err := s.GetOrderAdmin(ctx, claims, orderID)
	if err != nil {
		return "", err
	}
	if o.PurchaserEmail == "" {
		return "", apperr.Validation("This order has no email address to send to.")
	}
	if err := s.sendOrderConfirmationErr(ctx, o); err != nil {
		return "", apperr.Internal(err)
	}
	return o.PurchaserEmail, nil
}

func (s *Service) GetOrderAdmin(ctx context.Context, claims *auth.StaffClaims, orderID string) (*ordersapi.Order, error) {
	if !tenant.HasPermission(claims, tenant.PermOrdersView) {
		return nil, apperr.PermissionDenied("You do not have permission to view orders.")
	}
	o, err := s.store.GetOrderByID(ctx, orderID)
	if err != nil {
		return nil, notFoundOrInternal(err, "Order not found.")
	}
	if !tenant.CanAccessBranch(claims, o.BranchID) {
		return nil, apperr.PermissionDenied("You do not have access to this order.")
	}
	s.enrichItemModifiers(ctx, o)
	return o, nil
}

// enrichItemModifiers fills in missing modifier names and price deltas from the
// catalog for any item on the order that was stored with only an id (e.g. from
// a POS payload that sent only {"id":"..."}). Best-effort: catalog errors are
// silently ignored so a bad catalog wiring never breaks the order read.
func (s *Service) enrichItemModifiers(ctx context.Context, o *ordersapi.Order) {
	if s.catalog == nil {
		return
	}
	for i := range o.Items {
		item := &o.Items[i]
		// Skip if all modifiers already have names.
		needsEnrich := false
		for _, f := range item.SelectedFlavours {
			if f.Name == "" {
				needsEnrich = true
				break
			}
		}
		if !needsEnrich {
			for _, a := range item.SelectedAddons {
				if a.Name == "" {
					needsEnrich = true
					break
				}
			}
		}
		if !needsEnrich || item.ProductID == "" {
			continue
		}

		flavourIDs := make([]string, 0, len(item.SelectedFlavours))
		for _, f := range item.SelectedFlavours {
			if f.ID != "" {
				flavourIDs = append(flavourIDs, f.ID)
			}
		}
		addonIDs := make([]string, 0, len(item.SelectedAddons))
		for _, a := range item.SelectedAddons {
			if a.ID != "" {
				addonIDs = append(addonIDs, a.ID)
			}
		}

		priced, err := s.catalog.GetPricedItem(ctx, o.BranchID, item.ProductID, item.VariantID, flavourIDs, addonIDs, nil, nil, o.CreatedAt)
		if err != nil {
			continue
		}
		item.SelectedFlavours = enrichModifiers(item.SelectedFlavours, priced.SelectedFlavours)
		item.SelectedAddons = enrichModifiers(item.SelectedAddons, priced.SelectedAddons)
	}
}

// enrichModifiers fills Name and PriceDelta for any modifier that has an id
// but no name, using a map built from the catalog result.
func enrichModifiers(items []ordersapi.SelectedModifier, catalogMods []catalogapi.SelectedModifier) []ordersapi.SelectedModifier {
	if len(catalogMods) == 0 {
		return items
	}
	byID := make(map[string]catalogapi.SelectedModifier, len(catalogMods))
	for _, m := range catalogMods {
		byID[m.ID] = m
	}
	for i := range items {
		if items[i].Name == "" {
			if cm, ok := byID[items[i].ID]; ok {
				items[i].Name = cm.Name
				items[i].PriceDelta = cm.PriceDelta
			}
		}
	}
	return items
}

func (s *Service) AmendOrderAdmin(ctx context.Context, claims *auth.StaffClaims, orderID string, req AmendOrderRequest) (*ordersapi.Order, error) {
	if !tenant.HasPermission(claims, tenant.PermOrdersAmend) {
		return nil, apperr.PermissionDenied("You do not have permission to amend orders.")
	}
	if _, err := s.checkOrderScope(ctx, claims, orderID); err != nil {
		return nil, err
	}
	return s.AmendOrder(ctx, claims.StaffID, primaryRole(claims), orderID, req)
}

func (s *Service) CancelOrderAdmin(ctx context.Context, claims *auth.StaffClaims, orderID, reason string) (*CancellationOutcome, error) {
	if !tenant.HasPermission(claims, tenant.PermOrdersCancel) {
		return nil, apperr.PermissionDenied("You do not have permission to cancel orders.")
	}
	if _, err := s.checkOrderScope(ctx, claims, orderID); err != nil {
		return nil, err
	}
	return s.CancelOrder(ctx, "staff", &claims.StaffID, orderID, reason)
}

// MarkFulfilledAdmin closes an order out.
//
// Gated on orders.amend, not orders.view. Moving an order's status is a write,
// and gating it on the read permission let finance and franchise_viewer — both
// view-only by design, and both holders of orders.view — mark orders fulfilled.
func (s *Service) MarkFulfilledAdmin(ctx context.Context, claims *auth.StaffClaims, orderID, note string) error {
	if !tenant.HasPermission(claims, tenant.PermOrdersAmend) {
		return apperr.PermissionDenied("You do not have permission to update orders.")
	}
	o, err := s.checkOrderScope(ctx, claims, orderID)
	if err != nil {
		return err
	}
	if err := s.assertKitchenDone(ctx, o); err != nil {
		return err
	}
	return s.MarkFulfilled(ctx, claims.StaffID, orderID, note)
}

// SetStatusAdmin moves an order to an arbitrary status.
//
// Gated on orders.amend for the same reason as MarkFulfilledAdmin: this writes,
// so the read permission is the wrong gate. The kitchen's own progression
// surface is governed separately by kitchen.mark_status.
func (s *Service) SetStatusAdmin(ctx context.Context, claims *auth.StaffClaims, orderID, status, note string) error {
	if !tenant.HasPermission(claims, tenant.PermOrdersAmend) {
		return apperr.PermissionDenied("You do not have permission to update orders.")
	}
	o, err := s.checkOrderScope(ctx, claims, orderID)
	if err != nil {
		return err
	}
	// KDS: the console cancels through this endpoint too, so the same rule as
	// the cancel endpoints applies — nothing the kitchen has started.
	if o != nil && ordersapi.NormalizeStatus(status) == ordersapi.StatusCancelled && ordersapi.KOTInKitchen(o.KOTStatus) {
		return apperr.New("ORDER_IN_PREPARATION", apperr.CategoryBusinessRule,
			"The kitchen is already working on this order ("+o.KOTStatus+"); it cannot be cancelled.").
			WithMeta(map[string]any{"kotStatus": o.KOTStatus})
	}
	if ordersapi.NormalizeStatus(status) == ordersapi.StatusFulfilled {
		if err := s.assertKitchenDone(ctx, o); err != nil {
			return err
		}
	}
	if err := s.SetStatus(ctx, claims.StaffID, orderID, status, note); err != nil {
		return err
	}
	s.releaseStartedEarly(ctx, o, status)
	return nil
}

// assertKitchenDone refuses to mark a website order delivered while the KDS
// is still making it (queued or preparing at a store whose KDS is on).
func (s *Service) assertKitchenDone(ctx context.Context, o *ordersapi.Order) error {
	if o == nil || o.Channel != "web" || !s.kitchenStillWorking(ctx, o) {
		return nil
	}
	return apperr.New("KITCHEN_NOT_READY", apperr.CategoryBusinessRule,
		"The kitchen has not finished this order yet. Mark it delivered once it is ready on the KDS.").
		WithMeta(map[string]any{"kotStatus": o.KOTStatus})
}

/*
 * POS "Start Process" on a scheduled order sets it to confirmed. One due on
 * a later day is released to the kitchen now, so the KDS shows it today; the
 * till keeps it on the Scheduled tab until its day. Accepting a new website
 * order (placed -> confirmed) is not a start and releases nothing.
 */
func (s *Service) releaseStartedEarly(ctx context.Context, before *ordersapi.Order, status string) {
	if before == nil {
		return
	}
	switch ordersapi.NormalizeStatus(status) {
	case ordersapi.StatusCancelled, ordersapi.StatusScheduled, ordersapi.StatusReturned, ordersapi.StatusRefunded, ordersapi.StatusPlaced:
		return
	}
	if before.Status != ordersapi.StatusScheduled && before.Status != ordersapi.StatusConfirmed {
		return
	}
	released, err := s.store.ReleaseToKitchenIfAhead(ctx, before.ID)
	if err != nil {
		slog.Warn("order could not be released to the kitchen", "order", before.ID, "error", err)
		return
	}
	if released {
		s.publishOrderByID(ctx, realtime.EventOrderUpdated, before.ID)
	}
}

// refundableTotal is what an order can give back: its total less the tip,
// which is never refunded (returns, refunds and cancellations alike).
func refundableTotal(o *ordersapi.Order) int64 {
	return max(o.TotalCents-o.TipCents, 0)
}

// RefundOrderAdmin issues a refund against an order.
//
// Two permissions, and neither implies the other. orders.refund_limited
// covers a partial refund, orders.refund_full the whole order; counter_staff
// holds only the first and finance only the second. Both were declared in the
// matrix from the start, but only the limited one was ever checked — so
// counter_staff could refund any order in full, and finance's grant did
// nothing.
//
// The amount decides which applies, and the amount cannot be judged until the
// order is loaded, so those gates sit below the scope check.
//
// Per-assignment numeric limits (max_refund_cents, RBAC-03/RBAC-04) are still
// not enforced: they live in staff_role_assignments, the staff token does not
// carry them, and reading them here would need a seam into the identity
// service. Until that exists, the full/partial split is the control.
func (s *Service) RefundOrderAdmin(ctx context.Context, claims *auth.StaffClaims, orderID string, amountCents int64, reason string) (*CancellationOutcome, error) {
	// The two refund permissions are independent, not a ladder: finance holds
	// orders.refund_full without orders.refund_limited, so it may refund a
	// whole order but not part of one. Gating entry on the limited permission
	// alone would lock finance out of every refund it is entitled to make.
	canPartial := tenant.HasPermission(claims, tenant.PermOrdersRefundLimited)
	canFull := tenant.HasPermission(claims, tenant.PermOrdersRefundFull)
	if !canPartial && !canFull {
		return nil, apperr.PermissionDenied("You do not have permission to issue refunds.")
	}

	o, err := s.checkOrderScope(ctx, claims, orderID)
	if err != nil {
		return nil, err
	}
	if reason == "" {
		return nil, apperr.Validation("A reason is required for every refund.")
	}
	// The tip is the staff's, not the shop's sale: it is never refunded.
	refundable := refundableTotal(o)
	if amountCents <= 0 || amountCents > refundable {
		return nil, apperr.Validation("Refund amount must be between " + money.Format(1, o.Currency) + " and " + money.Format(refundable, o.Currency) + " (the order total less any tip).")
	}

	/*
	 * There has to be money to give back.
	 *
	 * Nothing checked. A cash-on-delivery order that had not been collected
	 * yet — no cash taken, nothing charged — could be "refunded" in full,
	 * marking the order as money-returned when none had ever arrived. The
	 * books then showed a refund against a payment that did not exist.
	 *
	 * Cancelling is the right action for an order nobody has paid for, and
	 * the message says so rather than leaving the operator to guess.
	 */
	if o.PaymentStatus != ordersapi.PaymentCaptured {
		if o.PaymentMethod == PaymentMethodCOD {
			return nil, apperr.New("NOTHING_TO_REFUND", apperr.CategoryBusinessRule,
				"No cash has been collected on this order yet, so there is nothing to refund. Cancel it instead.")
		}
		return nil, apperr.New("NOTHING_TO_REFUND", apperr.CategoryBusinessRule,
			"This order has not been paid, so there is nothing to refund. Cancel it instead.")
	}
	// Which permission applies is decided by the amount, so it can only be
	// settled once the order total is known.
	if amountCents == refundable && !canFull {
		return nil, apperr.PermissionDenied("You can refund part of this order, but not the full amount.")
	}
	if amountCents < refundable && !canPartial {
		return nil, apperr.PermissionDenied("You can refund this order in full, but not a part of it.")
	}

	// Only orders with a real charge go to Stripe. The rest are refunded by
	// hand and recorded here, which is what actually happens at a counter.
	if s.cards != nil {
		// To the cards that paid, as far as they go; anything beyond (a part
		// paid in cash) is handed back at the counter.
		outcome, err := s.refundToCards(ctx, o, "", amountCents, false, reason, claims.StaffID)
		if err != nil {
			return nil, err
		}
		if outcome == nil || outcome.RefundedCents < amountCents {
			slog.Info("refund recorded without a provider charge for part of it",
				"order", o.Reference, "amountCents", amountCents, "toCardsCents", refundedCents(outcome))
		}
	} else if refundableAtProvider(o.ProviderPaymentID) {
		if _, err := s.payments.Refund(ctx, o.ID, *o.ProviderPaymentID, amountCents, reason, claims.StaffID); err != nil {
			return nil, apperr.Wrap(err, "REFUND_FAILED", apperr.CategoryUpstream, "Refund could not be processed.")
		}
	} else {
		slog.Info("refund recorded without a provider charge",
			"order", o.Reference, "amountCents", amountCents, "reason", reason)
	}
	paymentStatus := ordersapi.PaymentPartialRefund
	if amountCents == refundable {
		paymentStatus = ordersapi.PaymentRefunded
	}
	if err := s.store.UpdatePaymentStatus(ctx, o.ID, paymentStatus); err != nil {
		return nil, apperr.Internal(err)
	}
	// The money has moved; live screens hear about it however this ends.
	defer s.publishOrderByID(ctx, realtime.EventOrderUpdated, o.ID)

	/*
	 * A full refund is a change to the order, not only to its ledger.
	 *
	 * Only the payment status used to move, so an order refunded in full
	 * still read as "confirmed" on every screen that shows an order status —
	 * including the kitchen queue, which would go on making it.
	 *
	 * A partial refund leaves the status alone: the order stands, the
	 * customer is owed some of it back, and it still has to be fulfilled.
	 */
	if amountCents == refundable {
		if _, err := s.store.UpdateOrderStatus(ctx, o.ID, ordersapi.StatusRefunded,
			"staff", &claims.StaffID, reason); err != nil {
			return nil, apperr.Internal(err)
		}
		if o.SlotCapacityID != nil {
			// The slot goes back: nobody is being served in it now.
			_ = s.releaseSlot(ctx, *o.SlotCapacityID)
		}
	}
	return &CancellationOutcome{RefundCents: amountCents, RefundPct: int(amountCents * 100 / o.TotalCents)}, nil
}

func (s *Service) checkOrderScope(ctx context.Context, claims *auth.StaffClaims, orderID string) (*ordersapi.Order, error) {
	o, err := s.store.GetOrderByID(ctx, orderID)
	if err != nil {
		return nil, notFoundOrInternal(err, "Order not found.")
	}
	if !tenant.CanAccessBranch(claims, o.BranchID) {
		return nil, apperr.PermissionDenied("You do not have access to this order.")
	}
	return o, nil
}

func primaryRole(claims *auth.StaffClaims) string {
	if len(claims.RoleAssignments) == 0 {
		return "staff"
	}
	return claims.RoleAssignments[0].Role
}

// --- Manual order creation (ADM-10, telephone orders) ------------------------

type ManualOrderItemRequest struct {
	ProductID      string
	VariantID      *string
	FlavourIDs     []string
	AddonIDs       []string
	PresentationID *string
	WrapColourID   *string
	Quantity       int
}

type CreateManualOrderRequest struct {
	// IdempotencyKey dedupes a retried submission. The handler supplies one
	// when the caller does not, because an absent key must not mean "dedupe
	// against every other order in this slot".
	IdempotencyKey       string
	BranchID             string
	FulfilmentType       string
	ScheduledDate        string
	SlotStart            string
	Recipient            Recipient
	GiftMessage          *string
	IsGift               bool
	Purchaser            Purchaser
	DeliveryStreet       *string
	DeliveryCity         *string
	DeliveryState        *string
	DeliveryPostcode     *string
	DeliveryInstructions *string
	Items                []ManualOrderItemRequest
	DeliveryAddress      *string
}

// CreateManualOrder builds a phone/counter order through the exact same
// cart -> checkout pipeline as the storefront, so it gets the same
// validation, pricing, and slot-reservation guarantees rather than a
// parallel code path (ADM-10).
func (s *Service) CreateManualOrder(ctx context.Context, claims *auth.StaffClaims, req CreateManualOrderRequest) (*ordersapi.Order, error) {
	if !tenant.HasPermission(claims, tenant.PermOrdersCreateManual) {
		return nil, apperr.PermissionDenied("You do not have permission to create orders.")
	}
	branch, err := s.branches.GetBranch(ctx, req.BranchID)
	if err != nil {
		return nil, apperr.NotFound("Branch not found.")
	}
	if !tenant.CanAccessBranch(claims, "", branch.ID) {
		return nil, apperr.PermissionDenied("You do not have access to this branch.")
	}
	if len(req.Items) == 0 {
		return nil, apperr.Validation("At least one item is required.")
	}
	if req.IdempotencyKey == "" {
		key, err := store.NewOrderReference()
		if err != nil {
			return nil, apperr.Internal(err)
		}
		req.IdempotencyKey = "manual_" + claims.StaffID + "_" + key
	}

	id := CartIdentity{GuestToken: "manual-" + claims.StaffID + "-" + req.BranchID}
	for _, item := range req.Items {
		if _, _, _, err := s.AddItem(ctx, id, AddItemRequest{
			BranchID: req.BranchID, ProductID: item.ProductID, VariantID: item.VariantID,
			FlavourIDs: item.FlavourIDs, AddonIDs: item.AddonIDs, PresentationID: item.PresentationID, WrapColourID: item.WrapColourID,
			Quantity: item.Quantity, IsGift: req.IsGift, RecipientFirst: &req.Recipient.FirstName, RecipientLast: &req.Recipient.LastName, GiftMessage: req.GiftMessage,
		}); err != nil {
			return nil, err
		}
	}

	// SetFulfilment takes (street, city, state, postcode, branch). These were
	// passed one place out, with the free-text address first, so the postcode
	// landed in the branch id and every delivery phone order failed with
	// "Branch not found". The single address line stands in for the street
	// when no street is given.
	street := req.DeliveryStreet
	if street == nil || strings.TrimSpace(*street) == "" {
		street = req.DeliveryAddress
	}
	branchID := req.BranchID
	if _, _, err := s.SetFulfilment(ctx, id, req.FulfilmentType,
		street, req.DeliveryCity, req.DeliveryState, req.DeliveryPostcode, &branchID); err != nil {
		return nil, err
	}
	date, err := timex.ParseDate(req.ScheduledDate)
	if err != nil {
		return nil, apperr.Validation("scheduledDate must be YYYY-MM-DD.")
	}
	if _, _, err := s.SetSchedule(ctx, id, date, req.SlotStart); err != nil {
		return nil, err
	}

	order, err := s.CreateOrder(ctx, id, CreateOrderRequest{
		Recipient: req.Recipient, GiftMessage: req.GiftMessage, IsGift: req.IsGift, Purchaser: req.Purchaser,
		DeliveryInstructions: req.DeliveryInstructions, PaymentMethodID: "manual_counter",
		// Unique per call.
		//
		// This used to be branch + date + slot, which meant two phone orders
		// for the same window were treated as a retry of one another: the
		// second silently returned the first order, and the second customer's
		// order was never created. A slot holds ten, so that is an ordinary
		// Saturday rather than an edge case.
		//
		// A caller that genuinely wants retry protection passes its own key;
		// this is the fallback for one that does not.
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		return nil, err
	}
	_, _ = s.store.UpdateOrderStatus(ctx, order.ID, ordersapi.StatusConfirmed, "staff", &claims.StaffID, "Manual order created by staff")
	return s.store.GetOrderByID(ctx, order.ID)
}

// RecordCashCollected settles a cash-on-delivery order.
//
// Gated on orders.amend rather than on a refund permission: this is the
// counter or the driver confirming money changed hands, which is the same
// class of act as marking an order fulfilled, and counter staff must be able
// to do it. Scope is checked so one branch cannot settle another's cash.
//
// The store guards the transition, so a second call — the driver and the
// counter both marking it, which is exactly how double-collection happens —
// returns a conflict rather than silently succeeding twice.
func (s *Service) RecordCashCollected(ctx context.Context, claims *auth.StaffClaims, orderID string) error {
	if !tenant.HasPermission(claims, tenant.PermOrdersAmend) {
		return apperr.PermissionDenied("You do not have permission to record payment against orders.")
	}
	order, err := s.checkOrderScope(ctx, claims, orderID)
	if err != nil {
		return err
	}
	if order.PaymentMethod != PaymentMethodCOD {
		return apperr.New("NOT_A_CASH_ORDER", apperr.CategoryBusinessRule,
			"This order was not placed as cash on delivery.")
	}

	updated, err := s.store.RecordCashCollected(ctx, orderID, claims.StaffID)
	if err != nil {
		return apperr.Internal(err)
	}
	if !updated {
		return apperr.New("CASH_ALREADY_COLLECTED", apperr.CategoryConflict,
			"This order has already been marked as paid.")
	}

	// Money changing hands is a financially significant action, so it is
	// attributable to a named person (NFR-11). Best-effort: the cash is
	// already recorded, and failing the request now would invite a second
	// collection attempt.
	if s.audit != nil {
		staffID := claims.StaffID
		_ = s.audit.Write(ctx, audit.Entry{
			ActorID: &staffID, ActorType: "staff", ActorRole: primaryRole(claims),
			Action: "order.cash_collected", ResourceType: "order", ResourceID: &orderID,
			BranchID:   &order.BranchID,
			AfterValue: map[string]any{"amountCents": order.TotalCents, "method": PaymentMethodCOD},
		})
	}
	s.publishOrderByID(ctx, realtime.EventOrderUpdated, orderID)
	return nil
}
