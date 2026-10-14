package domain

import (
	"context"
	"strings"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/services/orders/ordersapi"
)

// Bulk actions the till can take on a selection of orders.
const (
	BulkCancel = "cancel"
	BulkDelete = "delete"
	BulkStatus = "status"
)

// maxBulkOrders caps one request. Wide enough for a screen's worth of
// selected rows, narrow enough that a mistyped filter cannot walk the table.
const maxBulkOrders = 100

// BulkResult is what happened to one order in a batch.
type BulkResult struct {
	OrderID string `json:"orderId"`
	// What the till shows for the order (its bill number, else its
	// reference). Filled for failed rows so a message can name them.
	OrderNo string `json:"orderNo,omitempty"`
	OK      bool   `json:"ok"`
	// Set when OK is false: the same error code the single-order endpoint
	// would have returned, so the screen can explain each failed row.
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

/*
 * Acting on several orders at once.
 *
 * Each order is processed on its own and the outcome reported per order,
 * rather than the batch succeeding or failing as a unit. Selecting six orders
 * and having the whole thing rejected because one of them was already
 * cancelled is the wrong behaviour at a counter: the other five were valid
 * requests and the operator would have to work out which one was the problem
 * by elimination.
 *
 * So this never rolls back. A caller gets a list saying what happened to each,
 * and the screen can leave the failures selected with their reasons showing.
 *
 * Permission is checked per order by the single-order path each action
 * delegates to, not once up front — a selection can span outlets, and a bulk
 * endpoint must not become a way to act on an order the caller could not have
 * touched one at a time.
 */
func (s *Service) BulkOrderAction(ctx context.Context, claims *auth.StaffClaims, orderIDs []string, action, status, reason string) ([]BulkResult, error) {
	if len(orderIDs) == 0 {
		return nil, apperr.Validation("Select at least one order.").
			WithFields(apperr.FieldError{Field: "orderIds", Code: "REQUIRED", Message: "Required."})
	}
	if len(orderIDs) > maxBulkOrders {
		return nil, apperr.Validation("Too many orders in one request.").
			WithFields(apperr.FieldError{Field: "orderIds", Code: "MAX", Message: "At most 100."})
	}

	action = strings.TrimSpace(action)
	switch action {
	case BulkCancel, BulkDelete, BulkStatus:
	default:
		return nil, apperr.Validation("An action is cancel, delete or status.").
			WithFields(apperr.FieldError{Field: "action", Code: "INVALID", Message: "cancel, delete or status."})
	}
	if action == BulkStatus {
		if !ordersapi.IsValidStatus(status) {
			return nil, apperr.Validation("\"" + status + "\" is not an order status. Valid values: " +
				strings.Join(ordersapi.AllStatuses(), ", ") + ".").
				WithFields(apperr.FieldError{Field: "status", Code: "INVALID", Message: "Not a status."})
		}
	}

	// The same id twice would otherwise be attempted twice and report a
	// spurious "already cancelled" against the second.
	seen := map[string]bool{}
	out := make([]BulkResult, 0, len(orderIDs))

	for _, id := range orderIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true

		var err error
		switch action {
		case BulkCancel:
			_, err = s.CancelOrderAdmin(ctx, claims, id, reason)
		case BulkDelete:
			err = s.DeletePOSOrder(ctx, claims, id)
		case BulkStatus:
			err = s.SetStatusAdmin(ctx, claims, id, status, reason)
		}

		if err == nil {
			out = append(out, BulkResult{OrderID: id, OK: true})
			continue
		}
		r := BulkResult{OrderID: id, OrderNo: s.bulkOrderNo(ctx, id), Code: "INTERNAL_ERROR", Message: "An unexpected error occurred."}
		if ae, ok := apperr.As(err); ok {
			r.Code, r.Message = ae.Code, ae.Message
		}
		out = append(out, r)
	}

	return out, nil
}

// bulkOrderNo is the number the operator knows an order by. Best effort: a
// row that cannot be read is named by its id instead.
func (s *Service) bulkOrderNo(ctx context.Context, id string) string {
	o, err := s.store.GetOrderByID(ctx, id)
	if err != nil || o == nil {
		return id
	}
	if no := billNoLabel(o); no != "" {
		return no
	}
	if o.Reference != "" {
		return o.Reference
	}
	return id
}

// bulkReason is the short phrase for why one order was refused, for the
// summary line. Codes it does not know fall back to the error's own message.
func bulkReason(r BulkResult) string {
	switch r.Code {
	// The kitchen starting an order moves it to in_production, so cancel
	// refuses it with ORDER_IN_PRODUCTION before the KOT check is reached.
	// To the operator both mean the same thing.
	case "ORDER_IN_PREPARATION", "ORDER_IN_PRODUCTION":
		return "order in preparing"
	case "ORDER_ALREADY_CANCELLED":
		return "order already cancelled"
	case "ORDER_CLOSED":
		return "order already closed"
	case apperr.CodePermissionDenied:
		return "no permission"
	case apperr.CodeNotFound:
		return "order not found"
	}
	return strings.TrimRight(strings.TrimSpace(r.Message), ".")
}

// BulkFailureMessage is the one line the till shows when a cancel or delete
// did not go through for every selected order, e.g.
//
//	Order(s) 23, 43 not cancelled due to order in preparing.
//
// Orders are grouped by reason, in the order the reasons first appear. It
// returns "" when nothing failed.
func BulkFailureMessage(action string, results []BulkResult) string {
	verb := "updated"
	switch strings.TrimSpace(action) {
	case BulkCancel:
		verb = "cancelled"
	case BulkDelete:
		verb = "deleted"
	}

	var reasons []string
	byReason := map[string][]string{}
	for _, r := range results {
		if r.OK {
			continue
		}
		reason := bulkReason(r)
		if _, seen := byReason[reason]; !seen {
			reasons = append(reasons, reason)
		}
		no := r.OrderNo
		if no == "" {
			no = r.OrderID
		}
		byReason[reason] = append(byReason[reason], no)
	}
	if len(reasons) == 0 {
		return ""
	}

	parts := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		parts = append(parts, "Order(s) "+strings.Join(byReason[reason], ", ")+" not "+verb+" due to "+reason)
	}
	return strings.Join(parts, "; ") + "."
}
