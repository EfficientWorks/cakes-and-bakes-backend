package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/httpx"
	"bakehouse/services/orders/internal/domain"
	"bakehouse/services/payments/paymentsapi"
)

// --- Payment options ---------------------------------------------------------------

type paymentOptionsDTOType struct {
	BranchID string `json:"branchId"`
	Cash     bool   `json:"cash"`
	// A card run on the store's own terminal: recorded, never refunded
	// through an API.
	ManualCard bool `json:"manualCard"`
	// Online card providers for payment links: switched on for the store in
	// admin and configured on the server. Empty: no payment links.
	CardProviders []string `json:"cardProviders"`
}

// @Summary		Payment options for a store
// @Description	What the till can offer: cash, a manual card on the store's own terminal, and the online card providers (stripe) switched on for the store and configured on the server. Defaults to the store the POS signed in to.
// @Tags			pos
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	query		string	false	"Store (default: the POS session's store)"
// @Success		200			{object}	httpx.Envelope{data=paymentOptionsDTOType}
// @Router			/pos/payments/options [get]
func (h *Handler) POSPaymentOptions(w http.ResponseWriter, r *http.Request) {
	opts, err := h.svc.PaymentOptions(r.Context(), staffClaims(r), r.URL.Query().Get("branchId"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, paymentOptionsDTOType{
		BranchID: opts.BranchID, Cash: opts.Cash, ManualCard: opts.ManualCard, CardProviders: opts.CardProviders,
	})
}

// --- Payment links -------------------------------------------------------------------

type paymentLinkRequest struct {
	// stripe. Optional when the store has only one switched on.
	Provider string `json:"provider,omitempty" example:"stripe"`
	// The split share to pay; omitted pays what is owed on the whole order.
	BillID *string `json:"billId,omitempty"`
	// Optional; defaults to what is still owed (on the bill, if given).
	AmountCents int64 `json:"amountCents,omitempty" example:"8364"`
	// Where to send it; defaults to the order's customer email.
	Email string `json:"email,omitempty" example:"tina@example.com"`
	// Tip and service charge chosen on the payment page, already included in
	// amountCents. They are added to the order (and the split bill) so the
	// link can be for more than the order owed before them.
	TipCents           int64 `json:"tipCents,omitempty" example:"2000"`
	ServiceChargeCents int64 `json:"serviceChargeCents,omitempty" example:"0"`
}

type paymentLinkDTOType struct {
	ID          string  `json:"id"`
	OrderID     string  `json:"orderId"`
	BillID      *string `json:"billId"`
	Provider    string  `json:"provider"`
	URL         string  `json:"url"`
	AmountCents int64   `json:"amountCents"`
	Currency    string  `json:"currency"`
	Email       string  `json:"email"`
	// open | paid | expired | cancelled
	Status     string  `json:"status"`
	PaymentRef *string `json:"paymentRef"`
	SentCount  int     `json:"sentCount"`
	CreatedAt  string  `json:"createdAt"`
	PaidAt     *string `json:"paidAt"`
	// On send only: the same open link was sent again / an older one was
	// expired first.
	Resent   bool `json:"resent,omitempty"`
	Replaced bool `json:"replaced,omitempty"`
	// On send only: whether the email went to the mail service. When false,
	// emailError says so; share the url with the customer another way.
	EmailSent  *bool  `json:"emailSent,omitempty"`
	EmailError string `json:"emailError,omitempty"`
}

func paymentLinkDTO(l *paymentsapi.PaymentLink) paymentLinkDTOType {
	out := paymentLinkDTOType{
		ID: l.ID, OrderID: l.OrderID, BillID: l.BillID, Provider: l.Provider, URL: l.URL,
		AmountCents: l.AmountCents, Currency: l.Currency, Email: l.CustomerEmail, Status: l.Status,
		PaymentRef: l.PaymentRef, SentCount: l.SentCount, CreatedAt: l.CreatedAt.Format(time.RFC3339),
	}
	if l.PaidAt != nil {
		s := l.PaidAt.Format(time.RFC3339)
		out.PaidAt = &s
	}
	return out
}

// @Summary		Send a payment link
// @Description	Opens a hosted card payment page (Stripe Checkout) for what is owed on the order — or on one split bill — and emails it to the customer. One open link per order: asking again with the same provider, amount, bill and email re-sends it (resent=true); anything different expires the old link at the provider first (replaced=true). When the customer pays, the provider's webhook records the card payment (and settles the order when it covers the rest), and payment.completed is sent on /pos/events.
// @Tags			pos
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path		string				true	"orderId"
// @Param			body	body		paymentLinkRequest	false	"Request body"
// @Success		200		{object}	httpx.Envelope{data=paymentLinkDTOType}
// @Failure		400		{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		422		{object}	httpx.Envelope	"PROVIDER_NOT_ENABLED | NOTHING_OWED | ORDER_CLOSED"
// @Failure		503		{object}	httpx.Envelope	"PAYMENT_LINK_FAILED"
// @Router			/pos/orders/{orderId}/payment-link [post]
func (h *Handler) SendPaymentLink(w http.ResponseWriter, r *http.Request) {
	var req paymentLinkRequest
	if r.ContentLength != 0 {
		if err := httpx.DecodeJSON(r, &req); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
	}
	res, err := h.svc.SendPaymentLink(r.Context(), staffClaims(r), chi.URLParam(r, "orderId"), domain.PaymentLinkRequest{
		Provider: req.Provider, BillID: req.BillID, AmountCents: req.AmountCents, Email: req.Email,
		TipCents: req.TipCents, ServiceChargeCents: req.ServiceChargeCents,
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out := paymentLinkDTO(res.Link)
	out.Resent, out.Replaced = res.Resent, res.Replaced
	out.EmailSent, out.EmailError = &res.EmailSent, res.EmailError
	httpx.OK(w, out)
}

// @Summary		The order's payment link
// @Description	The most recent payment link for the order, in any status (open, paid, expired, cancelled).
// @Tags			pos
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path		string	true	"orderId"
// @Success		200		{object}	httpx.Envelope{data=paymentLinkDTOType}
// @Failure		404		{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/pos/orders/{orderId}/payment-link [get]
func (h *Handler) GetPaymentLink(w http.ResponseWriter, r *http.Request) {
	link, err := h.svc.GetPaymentLink(r.Context(), staffClaims(r), chi.URLParam(r, "orderId"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, paymentLinkDTO(link))
}

// @Summary		Cancel the payment link
// @Description	Expires the order's open payment link at the provider so the customer can no longer pay through it. Send a new one with POST.
// @Tags			pos
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path		string	true	"orderId"
// @Success		200		{object}	httpx.Envelope{data=paymentLinkDTOType}
// @Failure		422		{object}	httpx.Envelope	"NO_OPEN_LINK"
// @Router			/pos/orders/{orderId}/payment-link [delete]
func (h *Handler) CancelPaymentLink(w http.ResponseWriter, r *http.Request) {
	link, err := h.svc.CancelPaymentLink(r.Context(), staffClaims(r), chi.URLParam(r, "orderId"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, paymentLinkDTO(link))
}

// --- Return refunds ----------------------------------------------------------------

type returnRefundRequest struct {
	// cash | card | original (back the way it was paid: Stripe
	// payments first, the rest cash — for split payments)
	PaymentMode string `json:"paymentMode" example:"original"`
	// stripe | manual (card only). Stripe refunds through the provider
	// before the return is recorded; manual = own terminal.
	CardType string `json:"cardType,omitempty" example:"stripe"`
	// full (default): the return's refund. partial: amountCents of it.
	RefundType  string `json:"refundType,omitempty" example:"full"`
	AmountCents int64  `json:"amountCents,omitempty" example:"500"`
}

type refundLineDTOType struct {
	Provider    string `json:"provider"`
	PaymentRef  string `json:"paymentRef"`
	RefundRef   string `json:"refundRef"`
	AmountCents int64  `json:"amountCents"`
	Status      string `json:"status"`
}

type returnRefundDTOType struct {
	PaymentMode string `json:"paymentMode"`
	CardType    string `json:"cardType,omitempty"`
	RefundType  string `json:"refundType"`
	AmountCents int64  `json:"amountCents"`
	// Of amountCents, what to hand back in cash at the counter.
	CashCents int64 `json:"cashCents"`
	// succeeded | pending (settles by webhook: refund.completed / refund.failed
	// on /pos/events) | manual (handed back at the counter)
	Status string              `json:"status"`
	Lines  []refundLineDTOType `json:"lines"`
}

func returnRefundDTO(r *domain.ReturnRefundResult) *returnRefundDTOType {
	if r == nil {
		return nil
	}
	out := &returnRefundDTOType{
		PaymentMode: r.PaymentMode, CardType: r.CardType, RefundType: r.RefundType,
		AmountCents: r.AmountCents, CashCents: r.CashCents, Status: r.Status, Lines: []refundLineDTOType{},
	}
	if r.Outcome != nil {
		for _, l := range r.Outcome.Lines {
			out.Lines = append(out.Lines, refundLineDTOType{
				Provider: l.Provider, PaymentRef: l.PaymentRef, RefundRef: l.RefundRef,
				AmountCents: l.AmountCents, Status: l.Status,
			})
		}
	}
	return out
}
