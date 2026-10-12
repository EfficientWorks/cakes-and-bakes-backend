package domain

import (
	"context"
	"errors"
	"strings"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/realtime"
	"bakehouse/pkg/tenant"
	"bakehouse/services/orders/internal/store"
	"bakehouse/services/orders/ordersapi"
)

// ================================================================== KOT moves

func kotTargetForAction(action, from, fulfilment string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case ordersapi.KDSActionStart:
		return ordersapi.KOTPreparing, nil
	case ordersapi.KDSActionMarkReady, "ready":
		return ordersapi.KOTReady, nil
	case ordersapi.KDSActionPacking:
		return ordersapi.KOTPacking, nil
	case ordersapi.KDSActionReadyForDl:
		return ordersapi.KOTReadyForDelivery, nil
	case ordersapi.KDSActionHandedOver, "handover", "hand_over":
		if fulfilment == ordersapi.FulfilmentDelivery {
			return ordersapi.KOTOutForDelivery, nil
		}
		return ordersapi.KOTCompleted, nil
	case ordersapi.KDSActionDelivered:
		return ordersapi.KOTDelivered, nil
	case ordersapi.KDSActionRecall:
		return ordersapi.KOTReady, nil
	case ordersapi.KDSActionUndo:
		switch from {
		case ordersapi.KOTPreparing:
			return ordersapi.KOTQueue, nil
		case ordersapi.KOTReady:
			return ordersapi.KOTPreparing, nil
		}
		return "", apperr.New("INVALID_KOT_TRANSITION", apperr.CategoryBusinessRule, "Nothing to undo from "+from+".")
	}
	return "", apperr.Validation("Unknown action. Use start, mark_ready, packing, ready_for_delivery, handed_over, delivered, recall or undo.").
		WithFields(apperr.FieldError{Field: "action", Code: "INVALID", Message: "Unknown action."})
}

// KDSOrderKOTResult is the outcome of a KOT move.
type KDSOrderKOTResult struct {
	Order          *KDSOrderView
	FromKOTStatus  string
	ToKOTStatus    string
	OrderStatus    string
	OrderFulfilled bool
}

/*
 * UpdateOrderKOT moves an order through the kitchen and keeps the order
 * status in step, through the same SetStatus path the admin uses (so the
 * status timeline, "ready" / "delivered" emails and the sales roll-up all
 * happen exactly as they do today):
 *
 *   start (→ preparing)      order → in_production (if not yet in production)
 *   mark_ready (→ ready)     order → ready
 *   handed_over / delivered  paid order → fulfilled; unpaid delivery → out_for_delivery;
 *                            unpaid pickup/takeaway stays ready and is fulfilled by the POS payment
 *   delivered                refused until the payment is complete
 *   recall (→ ready)         kitchen status only; the order status is left alone
 */
func (s *Service) UpdateOrderKOT(ctx context.Context, claims *auth.StaffClaims, orderID, action, kotStatus, note string) (*KDSOrderKOTResult, error) {
	if !tenant.HasPermission(claims, tenant.PermKitchenMarkStatus) {
		return nil, apperr.PermissionDenied("You do not have permission to update kitchen status.")
	}
	o, err := s.store.GetOrderByID(ctx, orderID)
	if err != nil {
		return nil, notFoundOrInternal(err, "Order not found.")
	}
	if !s.kdsMayActOnBranch(ctx, claims, o.BranchID) {
		return nil, apperr.NotFound("Order not found.")
	}
	if o.Status == ordersapi.StatusCancelled || o.KOTStatus == ordersapi.KOTCancelled {
		return nil, apperr.New("ORDER_CANCELLED", apperr.CategoryBusinessRule, "This order was cancelled. Do not prepare it.")
	}
	if o.Status == ordersapi.StatusReturned || o.Status == ordersapi.StatusRefunded {
		return nil, apperr.New("ORDER_CLOSED", apperr.CategoryBusinessRule, "This order is closed.")
	}

	from := o.KOTStatus
	var to string
	switch {
	case strings.TrimSpace(action) != "":
		to, err = kotTargetForAction(action, from, o.FulfilmentType)
		if err != nil {
			return nil, err
		}
	case strings.TrimSpace(kotStatus) != "":
		to = ordersapi.NormalizeKOTStatus(kotStatus)
		if !ordersapi.IsValidKOTStatus(to) || to == ordersapi.KOTCancelled {
			return nil, apperr.Validation("\"" + kotStatus + "\" is not a kitchen status the KDS can set. Valid: " +
				strings.Join(ordersapi.AllKOTStatuses()[:8], ", ") + ".").
				WithFields(apperr.FieldError{Field: "kotStatus", Code: "INVALID", Message: "Not a KOT status."})
		}
	default:
		return nil, apperr.Validation("Send an action or a kotStatus.").
			WithFields(apperr.FieldError{Field: "action", Code: "REQUIRED", Message: "Required."})
	}
	if from == to {
		return nil, apperr.New("INVALID_KOT_TRANSITION", apperr.CategoryBusinessRule, "The order is already "+to+".")
	}
	if !ordersapi.CanMoveKOT(from, to) {
		return nil, apperr.New("INVALID_KOT_TRANSITION", apperr.CategoryBusinessRule,
			"An order cannot go from "+from+" to "+to+". From "+from+" it can go to: "+
				strings.Join(ordersapi.NextKOTStatuses(from), ", ")+".").
			WithMeta(map[string]any{"from": from, "to": to, "allowed": ordersapi.NextKOTStatuses(from)})
	}
	paid := o.PaymentStatus == ordersapi.PaymentCaptured
	if to == ordersapi.KOTDelivered && !paid {
		return nil, apperr.New("PAYMENT_PENDING", apperr.CategoryBusinessRule,
			"Take the payment before marking this order delivered.")
	}

	actor := store.KOTActor{ID: staffIDPtr(claims), Type: "staff", Source: "kds", Note: note}
	if err := s.store.MoveOrderKOT(ctx, orderID, from, to, actor); err != nil {
		if errors.Is(err, store.ErrKOTConflict) {
			return nil, apperr.New("KOT_STATUS_CHANGED", apperr.CategoryConflict,
				"This order was updated on another screen. Refresh and try again.")
		}
		return nil, apperr.Internal(err)
	}

	// Keep the order status in step.
	actorID := claims.StaffID
	target := ""
	switch to {
	case ordersapi.KOTPreparing:
		if ordersapi.IsAmendable(o.Status) {
			target = ordersapi.StatusInProduction
		}
	case ordersapi.KOTQueue:
		if o.Status == ordersapi.StatusInProduction {
			target = ordersapi.StatusConfirmed
		}
	case ordersapi.KOTReady:
		if from == ordersapi.KOTPreparing || from == ordersapi.KOTQueue {
			if ordersapi.IsAmendable(o.Status) || o.Status == ordersapi.StatusInProduction {
				target = ordersapi.StatusReady
			}
		}
	case ordersapi.KOTOutForDelivery, ordersapi.KOTDelivered, ordersapi.KOTCompleted:
		switch {
		case paid:
			if o.Status != ordersapi.StatusFulfilled {
				target = ordersapi.StatusFulfilled
			}
		case o.FulfilmentType == ordersapi.FulfilmentDelivery:
			if o.Status != ordersapi.StatusOutForDelivery {
				target = ordersapi.StatusOutForDelivery
			}
		default:
			if ordersapi.IsAmendable(o.Status) || o.Status == ordersapi.StatusInProduction {
				target = ordersapi.StatusReady
			}
		}
	}
	if target != "" {
		msg := "Kitchen: " + strings.ReplaceAll(to, "_", " ")
		if err := s.SetStatus(ctx, actorID, orderID, target, msg); err != nil {
			return nil, err
		}
	}
	/*
	 * Recall reopens the order too. A paid order (every website order) was
	 * fulfilled when it was handed over, and the board files a fulfilled
	 * order as done whatever its kitchen status, so recalling only the
	 * kitchen status left it there. Written straight to the status, not
	 * through SetStatus: a recall is not a fresh "ready", so no ready email
	 * and no second courier.
	 */
	if to == ordersapi.KOTReady && ordersapi.KOTHandedOff(from) &&
		(o.Status == ordersapi.StatusFulfilled || o.Status == ordersapi.StatusOutForDelivery) {
		if _, err := s.store.UpdateOrderStatus(ctx, orderID, ordersapi.StatusReady, "staff", &actorID, "Kitchen: recalled"); err != nil {
			return nil, apperr.Internal(err)
		}
		s.publishOrderByID(ctx, realtime.EventOrderStatusChanged, orderID)
	}

	view, _, err := s.GetKDSOrder(ctx, claims, orderID)
	if err != nil {
		return nil, err
	}
	s.publishOrder(realtime.EventOrderKOTChanged, &view.Order, "")
	return &KDSOrderKOTResult{
		Order: view, FromKOTStatus: from, ToKOTStatus: to,
		OrderStatus: view.Status, OrderFulfilled: view.Status == ordersapi.StatusFulfilled,
	}, nil
}

// KDSItemKOTResult is the outcome of a line move.
type KDSItemKOTResult struct {
	Order         *KDSOrderView
	ItemID        string
	FromKOTStatus string
	ToKOTStatus   string
	OrderStarted  bool
	AllItemsReady bool
}

// UpdateItemKOT moves one line: start (queue → preparing), mark_ready
// (preparing → ready), undo. Starting the first line starts the order.
func (s *Service) UpdateItemKOT(ctx context.Context, claims *auth.StaffClaims, orderID, itemID, action, kotStatus, note string) (*KDSItemKOTResult, error) {
	if !tenant.HasPermission(claims, tenant.PermKitchenMarkStatus) {
		return nil, apperr.PermissionDenied("You do not have permission to update kitchen status.")
	}
	o, err := s.store.GetOrderByID(ctx, orderID)
	if err != nil {
		return nil, notFoundOrInternal(err, "Order not found.")
	}
	if !s.kdsMayActOnBranch(ctx, claims, o.BranchID) {
		return nil, apperr.NotFound("Order not found.")
	}
	if o.Status == ordersapi.StatusCancelled || o.KOTStatus == ordersapi.KOTCancelled {
		return nil, apperr.New("ORDER_CANCELLED", apperr.CategoryBusinessRule, "This order was cancelled. Do not prepare it.")
	}
	if ordersapi.KOTHandedOff(o.KOTStatus) {
		return nil, apperr.New("ORDER_HANDED_OVER", apperr.CategoryBusinessRule, "This order has already left the kitchen.")
	}
	var item *ordersapi.OrderItem
	for i := range o.Items {
		if o.Items[i].ID == itemID {
			item = &o.Items[i]
		}
	}
	if item == nil {
		return nil, apperr.NotFound("Order item not found.")
	}
	if item.KOTStatus == ordersapi.ItemKOTCancelled ||
		item.ProductionStatus == ordersapi.ItemCancelled || item.ProductionStatus == ordersapi.ItemCancel {
		return nil, apperr.New("ITEM_CANCELLED", apperr.CategoryBusinessRule, "This item was cancelled. Do not prepare it.")
	}

	from := item.KOTStatus
	var to string
	switch a := strings.ToLower(strings.TrimSpace(action)); {
	case a == ordersapi.KDSActionStart:
		to = ordersapi.ItemKOTPreparing
	case a == ordersapi.KDSActionMarkReady || a == "ready":
		to = ordersapi.ItemKOTReady
	case a == ordersapi.KDSActionUndo:
		switch from {
		case ordersapi.ItemKOTPreparing:
			to = ordersapi.ItemKOTQueue
		case ordersapi.ItemKOTReady:
			to = ordersapi.ItemKOTPreparing
		}
	case a != "":
		return nil, apperr.Validation("Unknown action. Use start, mark_ready or undo.").
			WithFields(apperr.FieldError{Field: "action", Code: "INVALID", Message: "Unknown action."})
	case strings.TrimSpace(kotStatus) != "":
		to = ordersapi.NormalizeKOTStatus(kotStatus)
		if !ordersapi.IsValidItemKOTStatus(to) || to == ordersapi.ItemKOTCancelled {
			return nil, apperr.Validation("\"" + kotStatus + "\" is not an item kitchen status. Valid: queue, preparing, ready, completed.").
				WithFields(apperr.FieldError{Field: "kotStatus", Code: "INVALID", Message: "Not an item KOT status."})
		}
	default:
		return nil, apperr.Validation("Send an action or a kotStatus.").
			WithFields(apperr.FieldError{Field: "action", Code: "REQUIRED", Message: "Required."})
	}
	if to == "" || !ordersapi.CanMoveItemKOT(from, to) {
		return nil, apperr.New("INVALID_KOT_TRANSITION", apperr.CategoryBusinessRule,
			"An item cannot go from "+from+" to "+to+". From "+from+" it can go to: "+
				strings.Join(ordersapi.NextItemKOTStatuses(from), ", ")+".").
			WithMeta(map[string]any{"from": from, "to": to, "allowed": ordersapi.NextItemKOTStatuses(from)})
	}

	actor := store.KOTActor{ID: staffIDPtr(claims), Type: "staff", Source: "kds", Note: note}
	started, err := s.store.MoveItemKOT(ctx, orderID, itemID, from, to, actor)
	if err != nil {
		if errors.Is(err, store.ErrKOTConflict) {
			return nil, apperr.New("KOT_STATUS_CHANGED", apperr.CategoryConflict,
				"This item was updated on another screen. Refresh and try again.")
		}
		return nil, apperr.Internal(err)
	}
	if started && ordersapi.IsAmendable(o.Status) {
		if err := s.SetStatus(ctx, claims.StaffID, orderID, ordersapi.StatusInProduction, "Kitchen: preparing"); err != nil {
			return nil, err
		}
	}

	view, _, err := s.GetKDSOrder(ctx, claims, orderID)
	if err != nil {
		return nil, err
	}
	allReady := true
	for _, it := range view.Items {
		if it.KOTStatus == ordersapi.ItemKOTCancelled || it.ProductionStatus == ordersapi.ItemCancelled || it.ProductionStatus == ordersapi.ItemCancel {
			continue
		}
		if it.KOTStatus != ordersapi.ItemKOTReady && it.KOTStatus != ordersapi.ItemKOTCompleted {
			allReady = false
		}
	}

	/*
	 * The last item ready makes the order ready.
	 *
	 * Readying items one by one used to leave the order "preparing" until
	 * someone also pressed the order's Mark Ready: the ticket stayed on the
	 * active rail, the customer was never told, and a delivery was never sent
	 * to the courier (which happens when the order becomes ready).
	 */
	if allReady && to == ordersapi.ItemKOTReady && view.KOTStatus == ordersapi.KOTPreparing {
		readyActor := actor
		readyActor.Note = "All items ready"
		if err := s.store.MoveOrderKOT(ctx, orderID, ordersapi.KOTPreparing, ordersapi.KOTReady, readyActor); err == nil {
			if current, err := s.store.GetOrderByID(ctx, orderID); err == nil &&
				(ordersapi.IsAmendable(current.Status) || current.Status == ordersapi.StatusInProduction) {
				if err := s.SetStatus(ctx, claims.StaffID, orderID, ordersapi.StatusReady, "Kitchen: ready"); err != nil {
					return nil, err
				}
			}
			if refreshed, _, err := s.GetKDSOrder(ctx, claims, orderID); err == nil {
				view = refreshed
			}
			s.publishOrder(realtime.EventOrderKOTChanged, &view.Order, "")
		} else if !errors.Is(err, store.ErrKOTConflict) {
			return nil, apperr.Internal(err)
		}
	}
	s.publishOrder(realtime.EventOrderItemKOT, &view.Order, itemID)
	return &KDSItemKOTResult{Order: view, ItemID: itemID, FromKOTStatus: from, ToKOTStatus: to,
		OrderStarted: started, AllItemsReady: allReady}, nil
}

// AcknowledgeAmendment clears an order's Amended flag on the KDS.
func (s *Service) AcknowledgeAmendment(ctx context.Context, claims *auth.StaffClaims, orderID string) (*KDSOrderView, error) {
	o, err := s.store.GetOrderByID(ctx, orderID)
	if err != nil {
		return nil, notFoundOrInternal(err, "Order not found.")
	}
	if !s.kdsMayActOnBranch(ctx, claims, o.BranchID) {
		return nil, apperr.NotFound("Order not found.")
	}
	if !o.IsAmended || o.AmendedAcknowledged {
		return nil, apperr.New("NOTHING_TO_ACKNOWLEDGE", apperr.CategoryBusinessRule, "This order has no unacknowledged amendment.")
	}
	if _, err := s.store.AcknowledgeAmendment(ctx, orderID, staffIDPtr(claims)); err != nil {
		return nil, apperr.Internal(err)
	}
	view, _, err := s.GetKDSOrder(ctx, claims, orderID)
	if err != nil {
		return nil, err
	}
	s.publishOrder(realtime.EventOrderUpdated, &view.Order, "")
	return view, nil
}
