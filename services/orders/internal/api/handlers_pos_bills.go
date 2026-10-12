package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/httpx"
	"bakehouse/services/orders/internal/domain"
	"bakehouse/services/orders/ordersapi"
)

type posSplitRequest struct {
	Mode    string   `json:"mode"`
	Ways    int      `json:"ways,omitempty"`
	Amounts []int64  `json:"amounts,omitempty"`
	Labels  []string `json:"labels,omitempty"`
	// The service charge chosen on the bill screen. Set on the order before
	// it is split, so every share carries its part. Omitted: unchanged.
	ServiceChargeCents *int64 `json:"serviceChargeCents,omitempty" example:"500"`
	Groups             [][]struct {
		OrderItemID string `json:"orderItemId"`
		Quantity    int    `json:"quantity"`
	} `json:"groups,omitempty"`
}

// @Summary		Clear a bill split
// @Description	Removes every share of the split and puts the order back on one bill. Refused once part of the split has been paid.
// @Tags			pos
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path		string	true	"orderId"
// @Success		200		{object}	httpx.Envelope
// @Failure		403		{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404		{object}	httpx.Envelope	"NOT_FOUND"
// @Failure		422		{object}	httpx.Envelope	"SPLIT_REFUSED"
// @Router			/pos/orders/{orderId}/split [delete]
func (h *Handler) ClearSplit(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "orderId")
	removed, err := h.svc.ClearSplit(r.Context(), staffClaims(r), orderID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]any{"orderId": orderID, "cleared": true, "billsRemoved": removed})
}

// @Summary		Split a bill by amount, by item, or custom
// @Description	Replaces any previous split. Refused once part of the bill has been paid.
// @Tags			pos
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path	string	true	"orderId"
// @Param			body	body	posSplitRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/pos/orders/{orderId}/split [post]
func (h *Handler) SplitOrder(w http.ResponseWriter, r *http.Request) {
	var req posSplitRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	groups := make([][]ordersapi.BillItem, 0, len(req.Groups))
	for _, g := range req.Groups {
		items := make([]ordersapi.BillItem, 0, len(g))
		for _, it := range g {
			items = append(items, ordersapi.BillItem{OrderItemID: it.OrderItemID, Quantity: it.Quantity})
		}
		groups = append(groups, items)
	}

	bills, err := h.svc.SplitOrder(r.Context(), staffClaims(r), chi.URLParam(r, "orderId"),
		domain.SplitRequest{Mode: req.Mode, Ways: req.Ways, Amounts: req.Amounts, Labels: req.Labels, Groups: groups,
			ServiceChargeCents: req.ServiceChargeCents})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, billsDTO(bills, nil))
}

// @Summary		Read an order's bills and what each has taken
// @Tags			pos
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path	string	true	"orderId"
// @Success		200	{object}	httpx.Envelope
// @Router			/pos/orders/{orderId}/bills [get]
func (h *Handler) ListBills(w http.ResponseWriter, r *http.Request) {
	bills, payments, err := h.svc.ListBills(r.Context(), staffClaims(r), chi.URLParam(r, "orderId"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, billsDTO(bills, payments))
}

type posPaymentRequest struct {
	// Omit for a payment against the whole order rather than one share.
	BillID             *string `json:"billId,omitempty"`
	Method             string  `json:"method"`
	AmountCents        int64   `json:"amountCents"`
	TipCents           int64   `json:"tipCents,omitempty"`
	TenderedCents      *int64  `json:"tenderedCents,omitempty"`
	Reference          string  `json:"reference,omitempty"`
	ServiceChargeCents *int64  `json:"serviceChargeCents,omitempty"`
	PaidAt             *string `json:"paidAt,omitempty" example:"2026-09-16T10:33:52Z"`
}

// @Summary		Take a payment against a bill or the whole order
// @Description	Cash or card. Only cash reaches the drawer and counts towards the day close. When everything owed has been taken the order is marked paid.
// @Tags			pos
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path	string	true	"orderId"
// @Param			body	body	posPaymentRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/pos/orders/{orderId}/payments [post]
func (h *Handler) TakePayment(w http.ResponseWriter, r *http.Request) {
	var req posPaymentRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	billID := req.BillID
	if billID != nil && strings.TrimSpace(*billID) == "" {
		billID = nil
	}
	r, err := httpx.WithEventTime(r, "paidAt", req.PaidAt)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	payment, orderSettled, err := h.svc.TakePayment(r.Context(), staffClaims(r), chi.URLParam(r, "orderId"),
		ordersapi.BillPayment{
			BillID: billID, Method: req.Method, AmountCents: req.AmountCents,
			TipCents: req.TipCents, TenderedCents: req.TenderedCents, Reference: req.Reference,
		}, req.ServiceChargeCents)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	resp := map[string]any{
		"method":      payment.Method,
		"amountCents": payment.AmountCents,
		"tipCents":    payment.TipCents,
		// What to hand back, for cash tendered above what was owed.
		"changeCents":  payment.ChangeCents,
		"orderSettled": orderSettled,
	}
	if req.ServiceChargeCents != nil {
		resp["serviceChargeCents"] = *req.ServiceChargeCents
	}
	// The order as it now stands — status (fulfilled once settled), payment
	// method/type, tip and totals — so the till does not need a second read.
	if order, err := h.svc.GetOrderAdmin(r.Context(), staffClaims(r), chi.URLParam(r, "orderId")); err == nil {
		resp["orderStatus"] = order.Status
		resp["paymentStatus"] = order.PaymentStatus
		resp["paymentType"] = order.PaymentMethod
		resp["orderTotalCents"] = order.TotalCents
		resp["orderTipCents"] = order.TipCents
	}
	httpx.OK(w, resp)
}

func billsDTO(bills []ordersapi.Bill, payments []ordersapi.BillPayment) map[string]any {
	out := make([]map[string]any, 0, len(bills))
	for _, b := range bills {
		items := make([]map[string]any, 0, len(b.Items))
		for _, it := range b.Items {
			items = append(items, map[string]any{
				"orderItemId": it.OrderItemID,
				"productName": it.ProductName,
				"quantity":    it.Quantity,
				"amountCents": it.AmountCents,
			})
		}
		out = append(out, map[string]any{
			"id":          b.ID,
			"splitMode":   b.SplitMode,
			"label":       b.Label,
			"sequenceNo":  b.SequenceNo,
			"amountCents": b.AmountCents,
			"paidCents":   b.PaidCents,
			// What is still owed on this share, so the till does not subtract.
			"outstandingCents": maxInt64(b.AmountCents-b.PaidCents, 0),
			"status":           b.Status,
			"items":            items,
		})
	}

	paid := make([]map[string]any, 0, len(payments))
	for _, p := range payments {
		paid = append(paid, map[string]any{
			"id":            p.ID,
			"billId":        p.BillID,
			"method":        p.Method,
			"amountCents":   p.AmountCents,
			"tipCents":      p.TipCents,
			"tenderedCents": p.TenderedCents,
			"changeCents":   p.ChangeCents,
			"reference":     p.Reference,
			"takenBy":       p.TakenBy,
			"createdAt":     p.CreatedAt,
			// card: stripe (payment link) | manual (own terminal)
			"provider":      p.Provider,
			"providerRef":   p.ProviderRef,
			"paymentLinkId": p.PaymentLinkID,
		})
	}
	return map[string]any{"bills": out, "payments": paid}
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
