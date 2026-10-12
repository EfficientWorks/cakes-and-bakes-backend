package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/auth"
	"bakehouse/pkg/httpx"
)

func customerID(r *http.Request) string {
	claims, _ := auth.CustomerFromContext(r.Context())
	if claims == nil {
		return ""
	}
	return claims.CustomerID
}

// @Summary		List the customer's orders
// @Tags			account-orders
// @Produce		json
// @Security		BearerAuth
// @Param			page	query	int	false	"Page number (default 1)"
// @Param			limit	query	int	false	"Page size (default 50, max 200)"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Router			/account/orders [get]
func (h *Handler) ListMyOrders(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))

	orders, total, err := h.svc.ListOrdersForCustomer(r.Context(), customerID(r), q.Get("status"), page, limit)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out := make([]orderSummaryDTOType, 0, len(orders))
	for _, o := range orders {
		out = append(out, orderSummaryDTO(&o))
	}
	httpx.OKWithMeta(w, out, map[string]int{"page": maxInt(page, 1), "total": total})
}

// @Summary		Get one of the customer's orders
// @Tags			account-orders
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path	string	true	"orderId"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/account/orders/{orderId} [get]
func (h *Handler) GetMyOrder(w http.ResponseWriter, r *http.Request) {
	order, err := h.svc.GetOrderForCustomer(r.Context(), customerID(r), chi.URLParam(r, "orderId"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, orderDTO(order))
}

type cancelOrderRequest struct {
	Reason string `json:"reason"`
	// When the cancellation happened (staff endpoints only). Optional;
	// defaults to now.
	StatusAt *string `json:"statusAt,omitempty" example:"2026-09-16T10:33:52Z"`
}

// @Summary		Cancel an order before production starts
// @Tags			account-orders
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path	string	true	"orderId"
// @Param			body	body	cancelOrderRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/account/orders/{orderId}/cancel [post]
func (h *Handler) CancelMyOrder(w http.ResponseWriter, r *http.Request) {
	var req cancelOrderRequest
	_ = httpx.DecodeJSON(r, &req)

	cid := customerID(r)
	if _, err := h.svc.GetOrderForCustomer(r.Context(), cid, chi.URLParam(r, "orderId")); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	outcome, err := h.svc.CancelOrder(r.Context(), "customer", &cid, chi.URLParam(r, "orderId"), req.Reason)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]interface{}{
		"refundAmount": outcome.RefundCents, "refundMethod": "original_card",
		"message": refundMessage(outcome.RefundCents),
	})
}

func refundMessage(cents int64) string {
	if cents == 0 {
		return "This order is past the free-cancellation window; no automatic refund applies."
	}
	dollars := strconv.FormatFloat(float64(cents)/100, 'f', 2, 64)
	return "Refund of $" + dollars + " will appear in 5-10 business days."
}

// @Summary		Recreate a past order as a new cart
// @Tags			account-orders
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path	string	true	"orderId"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/account/orders/{orderId}/reorder [post]
func (h *Handler) ReorderMyOrder(w http.ResponseWriter, r *http.Request) {
	cid := customerID(r)
	cart, warnings, err := h.svc.Reorder(r.Context(), cid, chi.URLParam(r, "orderId"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	summary, _ := h.svc.PriceCart(r.Context(), cart)
	httpx.OK(w, map[string]interface{}{"cart": cartDTO(cart, summary), "warnings": warnings})
}

type claimOrderRequest struct {
	OrderReference string `json:"orderReference"`
	Email          string `json:"email"`
}

// @Summary		Attach a guest order to the signed-in account
// @Tags			account-orders
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body	claimOrderRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Router			/account/claim-order [post]
func (h *Handler) ClaimOrder(w http.ResponseWriter, r *http.Request) {
	var req claimOrderRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	order, err := h.svc.ClaimGuestOrder(r.Context(), customerID(r), req.OrderReference, req.Email)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]string{"orderId": order.ID})
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
