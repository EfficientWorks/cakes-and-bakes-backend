package api

import (
	"net/http"
	"strings"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/httpx"
	"bakehouse/services/orders/internal/domain"
)

// bulkOrderRequest acts on a selection of orders.
type bulkOrderRequest struct {
	OrderIDs []string `json:"orderIds"`
	// cancel, delete or status.
	Action string `json:"action"`
	// Required when action is status.
	Status string `json:"status,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// @Summary		Act on several orders at once
// @Description	Cancel, remove, or move a selection of orders to a status — for the tick boxes on the orders list. Each order is processed on its own, so one bad row does not stop the rest. For cancel and delete the response is a success only when every selected order went through; if any did not, it is an error (422 BULK_ACTION_INCOMPLETE) whose message names those orders and why, e.g. "Order(s) 23, 43 not cancelled due to order in preparing.", with `succeeded`, `failed` and per-order `results` under error.meta. For action=status the response always carries `results` per order. To mark a selection delivered, send action=status with status=fulfilled. At most 100 orders per call.
// @Tags			pos
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body		bulkOrderRequest	true	"Request body"
// @Success		200		{object}	httpx.Envelope
// @Failure		400		{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		403		{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		422		{object}	httpx.Envelope	"BULK_ACTION_INCOMPLETE — cancel/delete did not go through for every order"
// @Router			/pos/orders/bulk [post]
func (h *Handler) BulkPOSOrderAction(w http.ResponseWriter, r *http.Request) {
	var req bulkOrderRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	results, err := h.svc.BulkOrderAction(r.Context(), staffClaims(r), req.OrderIDs, req.Action, req.Status, req.Reason)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	succeeded := 0
	for _, x := range results {
		if x.OK {
			succeeded++
		}
	}
	failed := len(results) - succeeded

	// Cancel and delete: success only when every selected order went
	// through. The orders that could be cancelled/deleted still were — only
	// the response changes — and the message names the ones that were not.
	action := strings.TrimSpace(req.Action)
	if failed > 0 && (action == domain.BulkCancel || action == domain.BulkDelete) {
		httpx.WriteError(w, r, apperr.New("BULK_ACTION_INCOMPLETE", apperr.CategoryBusinessRule,
			domain.BulkFailureMessage(action, results)).
			WithMeta(map[string]any{
				"action":    action,
				"succeeded": succeeded,
				"failed":    failed,
				"results":   results,
			}))
		return
	}

	httpx.OK(w, map[string]any{
		"action":    req.Action,
		"succeeded": succeeded,
		"failed":    failed,
		"results":   results,
	})
}
