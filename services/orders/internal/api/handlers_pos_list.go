package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/httpx"
	"bakehouse/pkg/timex"
	"bakehouse/services/franchise/franchiseapi"
	"bakehouse/services/orders/internal/domain"
	"bakehouse/services/orders/ordersapi"
)

/*
 * The till's order reads.
 *
 * Delegating to the admin handlers, which are already branch-scoped by the
 * staff token: an order is an order, and a second listing that could drift
 * from the console's is a bug waiting to be found by whoever is holding a
 * receipt. They exist as named functions so /docs shows them under the till's
 * own prefix — a POS client should not have to discover its order list on an
 * /admin path.
 */

// @Summary		List the till's orders
// @Description	The branch's orders, newest first. Same filters as the console: status, search, dateFrom, dateTo, page, limit.
// @Tags			pos
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	query		string	false	"Branch (defaults to the token's scope)"
// @Param			status		query		string	false	"Filter by status"
// @Param			fulfilmentType	query	string	false	"Filter by fulfilment type (pickup, delivery, takeaway)"
// @Param			payment_type	query	string	false	"Filter by payment type (cash, card, all)"
// @Param			channel		query		string	false	"Filter by channel (pos, storefront, manual, all)"
// @Param			orderFilterType	query	string	false	"Preset tab, ANDed with the other filters: active, takeaway, delivery, pickup, online, completed"
// @Param			search		query		string	false	"Free-text search"
// @Param			dateFrom	query		string	false	"YYYY-MM-DD"
// @Param			dateTo		query		string	false	"YYYY-MM-DD"
// @Param			startDate	query		string	false	"YYYY-MM-DD (alias of dateFrom)"
// @Param			endDate		query		string	false	"YYYY-MM-DD (alias of dateTo)"
// @Param			kotStatus	query		string	false	"KDS kitchen status, comma-separated: queue, preparing, ready, packing, ready_for_delivery, out_for_delivery, delivered, completed, cancelled"
// @Param			page		query		int		false	"Page number (default 1)"
// @Param			limit		query		int		false	"Page size (default 50, max 200)"
// @Success		200			{object}	httpx.Envelope{data=[]orderSummaryDTOType}
// @Failure		401			{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403			{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/pos/orders [get]
func (h *Handler) ListPOSOrders(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))

	fulfilmentType := q.Get("fulfilmentType")
	if fulfilmentType == "" {
		fulfilmentType = q.Get("fulfilment_type")
	}

	paymentType := q.Get("payment_type")
	if paymentType == "" {
		paymentType = q.Get("paymentType")
	}
	if paymentType == "" {
		paymentType = q.Get("payment_method")
	}
	if paymentType == "" {
		paymentType = q.Get("paymentMethod")
	}

	channel := q.Get("channel")

	orderFilterType := strings.ToLower(strings.TrimSpace(q.Get("orderFilterType")))
	if orderFilterType == "" {
		orderFilterType = strings.ToLower(strings.TrimSpace(q.Get("order_filter_type")))
	}
	if orderFilterType != "" && orderFilterType != "all" {
		if _, _, _, _, ok := ordersapi.OrderFilterTypeConditions(orderFilterType); !ok {
			httpx.WriteError(w, r, apperr.Validation("orderFilterType must be one of: "+strings.Join(ordersapi.AllOrderFilterTypes(), ", ")+"."))
			return
		}
	} else {
		orderFilterType = ""
	}
	status := q.Get("status")
	for _, part := range strings.Split(status, ",") {
		v := ordersapi.NormalizeStatus(part)
		if v != "" && v != "all" && !ordersapi.IsValidStatus(v) && v != ordersapi.StatusReturned && v != ordersapi.StatusRefunded {
			httpx.WriteError(w, r, apperr.Validation("status must be one of: "+strings.Join(append(ordersapi.AllStatuses(), ordersapi.StatusReturned, ordersapi.StatusRefunded), ", ")+"."))
			return
		}
	}
	for _, part := range strings.Split(fulfilmentType, ",") {
		v := strings.ToLower(strings.TrimSpace(part))
		switch v {
		case "", "all", ordersapi.FulfilmentPickup, ordersapi.FulfilmentDelivery, ordersapi.FulfilmentTakeaway:
		default:
			httpx.WriteError(w, r, apperr.Validation("fulfilmentType must be one of: pickup, delivery, takeaway."))
			return
		}
	}

	f := ordersapi.ListOrdersFilter{
		BranchID:        q.Get("branchId"),
		Status:          status,
		FulfilmentType:  fulfilmentType,
		PaymentType:     paymentType,
		Channel:         channel,
		OrderFilterType: orderFilterType,
		Search:          q.Get("search"),
		Sort:            q.Get("sort"),
		Page:            page,
		Limit:           limit,
	}
	if v := q.Get("dateFrom"); v != "" {
		if t, err := timex.ParseDate(v); err == nil {
			f.DateFrom = &t
		}
	}
	if v := q.Get("dateTo"); v != "" {
		if t, err := timex.ParseDate(v); err == nil {
			f.DateTo = &t
		}
	}
	if err := applyKDSListFilters(q, &f); err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	orders, total, err := h.svc.ListOrdersAdmin(r.Context(), staffClaims(r), f)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	// Totals for everything the filter matched, not just this page.
	stats := mapOrderStats(orders, int64(total))
	if sum, err := h.svc.SummarizeOrdersAdmin(r.Context(), staffClaims(r), f); err == nil {
		applyListSummary(&stats, sum)
	}
	out := make([]orderSummaryDTOType, 0, len(orders))
	for _, o := range orders {
		out = append(out, orderSummaryDTO(&o))
	}

	p := page
	if p < 1 {
		p = 1
	}
	l := limit
	if l < 1 || l > 100 {
		l = 25
	}
	totalPages := 0
	if l > 0 {
		totalPages = (total + l - 1) / l
	}
	httpx.OKWithMetaAndStats(w, out, httpx.Meta{
		Page:       p,
		PerPage:    l,
		Total:      total,
		TotalPages: totalPages,
	}, stats)
}

// @Summary		Get one order at the till
// @Description	The full order — lines with their kitchen batch, totals, payments and returns.
// @Tags			pos
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path		string	true	"orderId"
// @Success		200		{object}	httpx.Envelope{data=orderDTOType}
// @Failure		403		{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404		{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/pos/orders/{orderId} [get]
func (h *Handler) GetPOSOrder(w http.ResponseWriter, r *http.Request) {
	h.GetOrderAdmin(w, r)
}

/*
 * Moving an order along from the till.
 *
 * These delegate to the admin handlers for the same reason the reads do: an
 * order's status means one thing, and a second path that could move it
 * differently is how a website order ends up "delivered" in one screen and
 * "ready" in another.
 *
 * They matter most for orders the till did not take. A website order arrives
 * already paid and has to be walked to fulfilled by whoever hands it over —
 * and until these existed the counter had no way to do that, because the only
 * status endpoints lived under /admin.
 */

// @Summary		Move an order to another status
// @Description	Any status in the workflow: placed, confirmed, scheduled, in_production, ready, out_for_delivery, fulfilled, cancelled. Use this to walk a website order to delivered from the counter.
// @Tags			pos
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path		string				true	"orderId"
// @Param			body	body		setStatusRequest	true	"Request body"
// @Success		200		{object}	httpx.Envelope
// @Failure		400		{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		403		{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404		{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/pos/orders/{orderId}/status [post]
func (h *Handler) SetPOSOrderStatus(w http.ResponseWriter, r *http.Request) {
	h.SetOrderStatusAdmin(w, r)
}

// @Summary		Mark an order delivered or collected
// @Description	Closes the order out and emails the customer. The one-tap version of setting status to fulfilled — which is what a counter does when it hands a website order over. The order must be ready or out for delivery. A cash order (cash, or cash on delivery) has its cash collected with it, so paymentStatus comes back captured; a card order must already be captured (PAYMENT_PENDING otherwise).
// @Tags			pos
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path		string	true	"orderId"
// @Success		200		{object}	httpx.Envelope
// @Failure		403		{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404		{object}	httpx.Envelope	"NOT_FOUND"
// @Param			body	body	markStatusRequest	false	"Request body (statusAt optional)"
// @Router			/pos/orders/{orderId}/mark-fulfilled [post]
func (h *Handler) MarkPOSOrderFulfilled(w http.ResponseWriter, r *http.Request) {
	var req markStatusRequest
	_ = httpx.DecodeJSON(r, &req)
	r, err := httpx.WithEventTime(r, "statusAt", req.StatusAt)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	paymentStatus, err := h.svc.MarkFulfilledPOS(r.Context(), staffClaims(r), chi.URLParam(r, "orderId"), req.Note)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]interface{}{
		"success":       true,
		"status":        ordersapi.StatusFulfilled,
		"paymentStatus": paymentStatus,
	})
}

// @Summary		Cancel an order
// @Description	The order did not happen. Distinct from removing a counter order, which says it never should have existed, and from a return, which says the goods came back. A reason is required.
// @Tags			pos
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path		string	true	"orderId"
// @Success		200		{object}	httpx.Envelope
// @Failure		400		{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		403		{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404		{object}	httpx.Envelope	"NOT_FOUND"
// @Param			body	body	cancelOrderRequest	true	"Request body (statusAt optional)"
// @Router			/pos/orders/{orderId}/cancel [post]
func (h *Handler) CancelPOSOrder(w http.ResponseWriter, r *http.Request) {
	h.CancelOrderAdmin(w, r)
}

// @Summary		Refund an order from the till
// @Description	Gives money back. A return records goods coming back and adjusts the order's totals; it moves no money, so a customer owed a refund needs this as well.
// @Tags			pos
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path		string	true	"orderId"
// @Success		200		{object}	httpx.Envelope
// @Failure		400		{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		403		{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404		{object}	httpx.Envelope	"NOT_FOUND"
// @Param			body	body	refundRequest	true	"Request body (statusAt optional)"
// @Router			/pos/orders/{orderId}/refund [post]
func (h *Handler) RefundPOSOrder(w http.ResponseWriter, r *http.Request) {
	h.RefundOrderAdmin(w, r)
}

func parsePOSDate(raw *string) *time.Time {
	if raw == nil {
		return nil
	}
	s := strings.TrimSpace(*raw)
	if s == "" {
		return nil
	}
	layouts := []string{
		"2006-01-02",
		"02 January 2006",
		"2 January 2006",
		"02 Jan 2006",
		"2 Jan 2006",
		time.RFC3339,
		"2006-01-02T15:04:05.000Z07:00",
		"2006-01-02T15:04:05Z07:00",
		"02/01/2006",
		"2006/01/02",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
			return &d
		}
	}
	return nil
}

func parsePOSDateStr(raw *string) *string {
	if raw == nil {
		return nil
	}
	if t := parsePOSDate(raw); t != nil {
		s := t.Format("2006-01-02")
		return &s
	}
	s := strings.TrimSpace(*raw)
	if s == "" {
		return nil
	}
	return &s
}

func parsePOSTime(raw *string) *string {
	if raw == nil {
		return nil
	}
	s := strings.TrimSpace(*raw)
	if s == "" {
		return nil
	}
	layouts := []string{
		"15:04:05",
		"15:04",
		"3:04 PM",
		"3:04PM",
		"03:04 PM",
		"03:04PM",
		"3:04 pm",
		"3:04pm",
		"03:04 pm",
		"03:04pm",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			out := t.Format("15:04:05")
			return &out
		}
	}
	return &s
}

type posDeliveryQuoteRequest struct {
	BranchID string `json:"branchId" example:"0c993b06-8dd2-4eb4-8380-8eb188a2f39f"`
	// The delivery address. Street is optional for a ZIP-priced store and
	// improves accuracy for a radius-priced one.
	Street   string `json:"street,omitempty" example:"53 Wilk Rd"`
	City     string `json:"city,omitempty" example:"Edison"`
	State    string `json:"state,omitempty" example:"NJ"`
	Postcode string `json:"postcode" example:"08837"`
	// Item subtotal before discounts, tip, tax and delivery: what "free over
	// $X" rules compare against.
	SubtotalCents int64 `json:"subtotalCents" example:"6800"`
	// The delivery slot's start ("HH:MM"), for charge-by-delivery-time rules.
	SlotStart string `json:"slotStart,omitempty" example:"14:00"`
}

// @Summary		Quote a delivery fee at the till
// @Description	The store's delivery pricing for an address, order size and slot. An address outside the store's area comes back with deliverable=false and a reason; the till may still take the order with a fee staff type in. Sending deliveryFeeCents 0 (or omitting it) on POST /pos/orders applies this quote automatically.
// @Tags			pos
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body		posDeliveryQuoteRequest	true	"Address, subtotal and slot"
// @Success		200		{object}	httpx.Envelope{data=franchiseapi.DeliveryQuote}
// @Failure		400		{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		403		{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404		{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/pos/delivery/quote [post]
func (h *Handler) POSDeliveryQuote(w http.ResponseWriter, r *http.Request) {
	var req posDeliveryQuoteRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	// Named here so swag can resolve the @Success type through this file's imports.
	var q *franchiseapi.DeliveryQuote
	q, err := h.svc.QuoteDeliveryPOS(r.Context(), staffClaims(r), domain.DeliveryQuoteRequest{
		BranchID: req.BranchID, Street: req.Street, City: req.City, State: req.State, Postcode: req.Postcode,
		SubtotalCents: req.SubtotalCents, SlotStart: req.SlotStart,
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, q)
}
