package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/httpx"
	"bakehouse/services/orders/ordersapi"
)

type posReturnRequest struct {
	FullReturn bool    `json:"fullReturn,omitempty"`
	Reason     string  `json:"reason,omitempty"`
	ReturnedAt *string `json:"returnedAt,omitempty" example:"2026-09-16T10:33:52Z"`
	Items      []struct {
		OrderItemID string `json:"orderItemId"`
		Quantity    int    `json:"quantity"`
	} `json:"items,omitempty"`

	// Discount on the goods coming back. Cents wins over the percentage;
	// omit both and the order's own discount is applied to what comes back.
	DiscountPct   *float64 `json:"discountPct,omitempty" example:"10"`
	DiscountCents *int64   `json:"discountCents,omitempty" example:"150"`
	// Tax on the goods coming back (after the discount). Cents wins over the
	// percentage; omit both and the order's tax rate is used.
	TaxPct   *float64 `json:"taxPct,omitempty" example:"6.625"`
	TaxCents *int64   `json:"taxCents,omitempty" example:"89"`

	// How the money goes back. Omitted: the return moves no money (as before).
	Refund *returnRefundRequest `json:"refund,omitempty"`
}

// posReturnResponse is the return just recorded.
// refundAmountCents = grossAmountCents - discountCents + taxCents.
type posReturnResponse struct {
	ReturnID string `json:"returnId"`
	// Gross value of the goods (unit price × quantity). Kept as amountCents
	// for existing clients.
	AmountCents      int64 `json:"amountCents"`
	GrossAmountCents int64 `json:"grossAmountCents"`
	returnChargesDTOType
	// Each item on this return with its own discount, tax and refund (item
	// based); the fields above are their totals (return based).
	Items []orderReturnItemDTOType `json:"items"`
	// The refund made, when the request asked for one.
	Refund *returnRefundDTOType `json:"refund,omitempty"`
	Order  orderDTOType         `json:"order"`
}

// @Summary		Return a whole order or some of its lines
// @Description	fullReturn (order based) hands back everything still outstanding; items (return based) hands back particular lines. discountPct / discountCents and taxPct / taxCents are the discount and tax on what comes back — cents wins over the percentage; omitted, the order's own discount and tax rate are applied. Saved on the return and totalled on the order; GET /pos/orders/{orderId} shows both (returns[] and returnSummary).
// @Tags			pos
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path	string	true	"orderId"
// @Param			body	body	posReturnRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope{data=posReturnResponse}
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/pos/orders/{orderId}/return [post]
func (h *Handler) ReturnPOSOrder(w http.ResponseWriter, r *http.Request) {
	var req posReturnRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	lines := make([]ordersapi.ReturnLine, 0, len(req.Items))
	for _, it := range req.Items {
		lines = append(lines, ordersapi.ReturnLine{OrderItemID: it.OrderItemID, Quantity: it.Quantity})
	}

	r, err := httpx.WithEventTime(r, "returnedAt", req.ReturnedAt)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	orderID := chi.URLParam(r, "orderId")
	var refundReq *ordersapi.ReturnRefund
	if req.Refund != nil {
		refundReq = &ordersapi.ReturnRefund{
			PaymentMode: req.Refund.PaymentMode, CardType: req.Refund.CardType,
			RefundType: req.Refund.RefundType, AmountCents: req.Refund.AmountCents,
		}
	}
	returnID, charges, refund, err := h.svc.ReturnPOSOrder(r.Context(), staffClaims(r), orderID,
		ordersapi.ReturnRequest{
			FullReturn: req.FullReturn, Reason: req.Reason, Items: lines,
			DiscountPct: req.DiscountPct, DiscountCents: req.DiscountCents,
			TaxPct: req.TaxPct, TaxCents: req.TaxCents, Refund: refundReq,
		})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out := posReturnResponse{
		ReturnID: returnID, AmountCents: charges.GrossCents, GrossAmountCents: charges.GrossCents,
		returnChargesDTOType: returnChargesDTO(charges),
		Items:                []orderReturnItemDTOType{},
		Refund:               returnRefundDTO(refund),
	}
	if order, err := h.svc.GetOrderAdmin(r.Context(), staffClaims(r), orderID); err == nil {
		out.Order = orderDTO(order)
		for _, ret := range out.Order.Returns {
			if ret.ID == returnID {
				out.Items = ret.Items
			}
		}
		h.enrichCustomerDTO(r.Context(), &out.Order.Customer, order.CustomerID)
	}
	httpx.OK(w, out)
}
