package api

import (
	"net/http"

	"bakehouse/pkg/httpx"
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
// @Description	Cancel, remove, or move a selection of orders to a status — for the tick boxes on the orders list. Each order is processed on its own and reported on separately, so one bad row does not reject the rest; check `results` per order rather than the overall status. To mark a selection delivered, send action=status with status=fulfilled. At most 100 orders per call.
// @Tags			pos
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body		bulkOrderRequest	true	"Request body"
// @Success		200		{object}	httpx.Envelope
// @Failure		400		{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		403		{object}	httpx.Envelope	"PERMISSION_DENIED"
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
	httpx.OK(w, map[string]any{
		"action":    req.Action,
		"succeeded": succeeded,
		"failed":    len(results) - succeeded,
		"results":   results,
	})
}
