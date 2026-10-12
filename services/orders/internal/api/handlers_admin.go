package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/httpx"
	"bakehouse/pkg/timex"
	"bakehouse/services/orders/internal/domain"
	"bakehouse/services/orders/ordersapi"
)

func staffClaims(r *http.Request) *auth.StaffClaims {
	c, _ := auth.StaffFromContext(r.Context())
	return c
}

// @Summary		List orders in scope
// @Tags			admin-orders
// @Produce		json
// @Security		BearerAuth
// @Param			status	query	string	false	"Filter by status"
// @Param			fulfilmentType	query	string	false	"Filter by fulfilment type (pickup, delivery, takeaway)"
// @Param			payment_type	query	string	false	"Filter by payment type (cash, card, all)"
// @Param			channel	query	string	false	"Filter by channel (pos, storefront, manual, all)"
// @Param			orderFilterType	query	string	false	"Preset tab, ANDed with the other filters: active, takeaway, delivery, pickup, online, completed"
// @Param			search	query	string	false	"Free-text search"
// @Param			dateFrom	query	string	false	"YYYY-MM-DD"
// @Param			dateTo	query	string	false	"YYYY-MM-DD"
// @Param			startDate	query	string	false	"YYYY-MM-DD (alias of dateFrom)"
// @Param			endDate	query	string	false	"YYYY-MM-DD (alias of dateTo)"
// @Param			kotStatus	query	string	false	"KDS kitchen status, comma-separated"
// @Param			page	query	int	false	"Page number (default 1)"
// @Param			limit	query	int	false	"Page size (default 50, max 200)"
// @Success		200	{object}	httpx.Envelope{data=[]orderSummaryDTOType}
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/admin/orders [get]
// @Summary		Resend an order confirmation email
// @Tags			admin-orders
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path	string	true	"orderId"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/orders/{orderId}/resend-confirmation [post]
func (h *Handler) ResendConfirmationAdmin(w http.ResponseWriter, r *http.Request) {
	to, err := h.svc.ResendConfirmationAdmin(r.Context(), staffClaims(r), chi.URLParam(r, "orderId"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]string{"sentTo": to})
}

type orderStatsDTOType struct {
	NewOrdersCount   int64 `json:"newOrdersCount"`
	TotalOrdersCount int64 `json:"totalOrdersCount"`
	// Net revenue: excludes cancelled orders and returned amounts.
	TotalRevenue    int64 `json:"totalRevenue"`
	CancelledOrders int64 `json:"cancelledOrders"`
	// Over the whole filtered set, not the page. Net, like totalRevenue.
	TotalPriceCents      int64   `json:"totalPriceCents"`
	TotalPrice           float64 `json:"totalPrice"`
	ScheduledOrdersCount int64   `json:"scheduledOrdersCount"`

	// What came off the total, so the screen can show the working:
	// grossRevenue = totalRevenue + cancelledAmount + returnedAmount.
	GrossRevenueCents    int64   `json:"grossRevenueCents"`
	GrossRevenue         float64 `json:"grossRevenue"`
	CancelledAmountCents int64   `json:"cancelledAmountCents"`
	CancelledAmount      float64 `json:"cancelledAmount"`
	ReturnedOrders       int64   `json:"returnedOrders"`
	ReturnedAmountCents  int64   `json:"returnedAmountCents"`
	ReturnedAmount       float64 `json:"returnedAmount"`
}

// applyListSummary overlays the whole-set totals onto the stats.
func applyListSummary(stats *orderStatsDTOType, sum ordersapi.OrderListSummary) {
	stats.TotalOrdersCount = int64(sum.TotalOrders)
	stats.TotalRevenue = sum.TotalCents
	stats.TotalPriceCents = sum.TotalCents
	stats.TotalPrice = float64(sum.TotalCents) / 100
	stats.ScheduledOrdersCount = int64(sum.ScheduledOrders)
	stats.CancelledOrders = int64(sum.CancelledOrders)
	stats.CancelledAmountCents = sum.CancelledCents
	stats.CancelledAmount = float64(sum.CancelledCents) / 100
	stats.ReturnedOrders = int64(sum.ReturnedOrders)
	stats.ReturnedAmountCents = sum.ReturnedCents
	stats.ReturnedAmount = float64(sum.ReturnedCents) / 100
	stats.GrossRevenueCents = sum.GrossCents
	stats.GrossRevenue = float64(sum.GrossCents) / 100
}

// orderClosedWithoutSale reports whether an order's total no longer counts
// as revenue: cancelled, fully returned or refunded.
func orderClosedWithoutSale(status string) bool {
	switch status {
	case ordersapi.StatusCancelled, ordersapi.StatusReturned, ordersapi.StatusRefunded:
		return true
	}
	return false
}

// mapOrderStats is the page-only fallback, used when the whole-set summary
// cannot be read. Same rules: revenue excludes cancelled and returned money.
func mapOrderStats(orders []ordersapi.Order, total int64) orderStatsDTOType {
	var stats orderStatsDTOType

	stats.TotalOrdersCount = total

	for _, order := range orders {
		if order.Status == "confirmed" {
			stats.NewOrdersCount++
		}

		if order.Status == ordersapi.StatusCancelled {
			stats.CancelledOrders++
			stats.CancelledAmountCents += order.TotalCents
			continue
		}
		if orderClosedWithoutSale(order.Status) {
			stats.ReturnedOrders++
			stats.ReturnedAmountCents += order.ReturnTotalCents + order.TotalCents
			continue
		}
		if order.ReturnTotalCents > 0 {
			stats.ReturnedOrders++
			stats.ReturnedAmountCents += order.ReturnTotalCents
		}
		stats.TotalRevenue += order.TotalCents
	}
	stats.TotalPriceCents = stats.TotalRevenue
	stats.TotalPrice = float64(stats.TotalRevenue) / 100
	stats.CancelledAmount = float64(stats.CancelledAmountCents) / 100
	stats.ReturnedAmount = float64(stats.ReturnedAmountCents) / 100
	stats.GrossRevenueCents = stats.TotalRevenue + stats.CancelledAmountCents + stats.ReturnedAmountCents
	stats.GrossRevenue = float64(stats.GrossRevenueCents) / 100

	return stats
}

func (h *Handler) ListOrdersAdmin(w http.ResponseWriter, r *http.Request) {
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

	f := ordersapi.ListOrdersFilter{
		BranchID: q.Get("branchId"), Status: q.Get("status"),
		FulfilmentType: fulfilmentType,
		PaymentType:    paymentType,
		Channel:        channel,
		Search:         q.Get("search"), Sort: q.Get("sort"), Page: page, Limit: limit,
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

	stats := mapOrderStats(orders, int64(total))
	if sum, err := h.svc.SummarizeOrdersAdmin(r.Context(), staffClaims(r), f); err == nil {
		applyListSummary(&stats, sum)
	}

	out := make([]orderSummaryDTOType, 0, len(orders))
	for _, o := range orders {
		out = append(out, orderSummaryDTO(&o))
	}
	httpx.OKWithMetaAndStats(w, out, map[string]int{"page": maxInt(page, 1), "total": total}, stats)
}

// Saved views (ADM-03): thin wrappers over the same filtered list.
//
//	@Summary		List orders scheduled for today
//	@Tags			admin-orders
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	httpx.Envelope
//	@Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
//	@Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
//	@Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
//	@Router			/admin/orders/views/today [get]
func (h *Handler) ListOrdersToday(w http.ResponseWriter, r *http.Request) {
	h.listOrdersView(w, r, ordersapi.ListOrdersFilter{Status: ""}, todayRange())
}

// @Summary		List orders scheduled for tomorrow
// @Tags			admin-orders
// @Produce		json
// @Security		BearerAuth
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/admin/orders/views/tomorrow [get]
func (h *Handler) ListOrdersTomorrow(w http.ResponseWriter, r *http.Request) {
	tomorrow := time.Now().AddDate(0, 0, 1)
	h.listOrdersView(w, r, ordersapi.ListOrdersFilter{}, [2]*time.Time{&tomorrow, &tomorrow})
}

// @Summary		List orders past their slot and not yet fulfilled
// @Tags			admin-orders
// @Produce		json
// @Security		BearerAuth
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/admin/orders/views/unfulfilled [get]
func (h *Handler) ListOrdersUnfulfilled(w http.ResponseWriter, r *http.Request) {
	branchID := r.URL.Query().Get("branchId")
	var out []orderSummaryDTOType
	for _, status := range []string{ordersapi.StatusPlaced, ordersapi.StatusConfirmed, ordersapi.StatusScheduled, ordersapi.StatusInProduction, ordersapi.StatusReady} {
		orders, _, err := h.svc.ListOrdersAdmin(r.Context(), staffClaims(r), ordersapi.ListOrdersFilter{BranchID: branchID, Status: status, Limit: 100})
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		for _, o := range orders {
			out = append(out, orderSummaryDTO(&o))
		}
	}
	if out == nil {
		out = []orderSummaryDTOType{}
	}
	httpx.OK(w, out)
}

func todayRange() [2]*time.Time {
	today := time.Now()
	return [2]*time.Time{&today, &today}
}

func (h *Handler) listOrdersView(w http.ResponseWriter, r *http.Request, base ordersapi.ListOrdersFilter, dateRange [2]*time.Time) {
	base.BranchID = r.URL.Query().Get("branchId")
	base.DateFrom, base.DateTo = dateRange[0], dateRange[1]
	base.Limit = 100
	orders, total, err := h.svc.ListOrdersAdmin(r.Context(), staffClaims(r), base)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out := make([]orderSummaryDTOType, 0, len(orders))
	for _, o := range orders {
		out = append(out, orderSummaryDTO(&o))
	}
	httpx.OKWithMeta(w, out, map[string]int{"total": total})
}

// @Summary		Get one order
// @Tags			admin-orders
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path	string	true	"orderId"
// @Success		200	{object}	httpx.Envelope{data=orderDTOType}
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/orders/{orderId} [get]
func (h *Handler) enrichCustomerDTO(ctx context.Context, c *customerOrderDTOType, customerID *string) {
	if customerID == nil || strings.TrimSpace(*customerID) == "" {
		return
	}
	cust := h.svc.GetCustomerDetails(ctx, *customerID)
	if cust == nil {
		return
	}
	// What was typed for this order wins; the customer's address book fills
	// only what the order does not have (orders from before the till kept
	// its own city and postcode, and website orders).
	if cust.Address != nil && (c.Address == nil || strings.TrimSpace(*c.Address) == "") {
		c.Address = cust.Address
	}
	if cust.City != nil && (c.City == nil || strings.TrimSpace(*c.City) == "") {
		c.City = cust.City
	}
	if cust.Postcode != nil && (c.Postcode == nil || strings.TrimSpace(*c.Postcode) == "") {
		c.Postcode = cust.Postcode
	}
	if cust.Street != nil && (c.Street == nil || strings.TrimSpace(*c.Street) == "") {
		c.Street = cust.Street
	}
	if cust.State != nil && (c.State == nil || strings.TrimSpace(*c.State) == "") {
		c.State = cust.State
	}
	if cust.MobileNumber != nil {
		c.MobileNumber = cust.MobileNumber
		if c.Phone == nil {
			c.Phone = cust.MobileNumber
		}
	}
	if cust.FirstName != nil && c.FirstName == "" {
		c.FirstName = *cust.FirstName
	}
	if cust.LastName != nil && c.LastName == "" {
		c.LastName = *cust.LastName
	}
	if cust.Email != nil && c.Email == "" {
		c.Email = *cust.Email
	}
	if cust.DOB != nil {
		c.DOB = cust.DOB
	}
	if cust.AnniversaryDate != nil {
		c.AnniversaryDate = cust.AnniversaryDate
	}
	c.Name = strings.TrimSpace(c.FirstName + " " + c.LastName)
}

func (h *Handler) GetOrderAdmin(w http.ResponseWriter, r *http.Request) {
	order, err := h.svc.GetOrderAdmin(r.Context(), staffClaims(r), chi.URLParam(r, "orderId"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	dto := orderDTO(order)
	h.enrichCustomerDTO(r.Context(), &dto.Customer, order.CustomerID)
	httpx.OK(w, dto)
}

type amendOrderRequest struct {
	RecipientFirstName   *string `json:"recipientFirstName"`
	RecipientLastName    *string `json:"recipientLastName"`
	RecipientPhone       *string `json:"recipientPhone"`
	GiftMessage          *string `json:"giftMessage"`
	DeliveryInstructions *string `json:"deliveryInstructions"`
	DeliveryStreet       *string `json:"deliveryStreet"`
	DeliveryCity         *string `json:"deliveryCity"`
	DeliveryState        *string `json:"deliveryState"`
	DeliveryPostcode     *string `json:"deliveryPostcode"`
	BusinessName         *string `json:"businessName"`
	DeliveryAddressType  *string `json:"deliveryAddressType" enums:"residence,business"`
	ScheduledDate        *string `json:"scheduledDate"`
	SlotStart            *string `json:"slotStart"`
	Reason               string  `json:"reason"`
	// KDS: flag the order "Amended" on the kitchen display.
	MarkAmended bool `json:"markAmended,omitempty"`
}

// @Summary		Amend an order before production starts
// @Tags			admin-orders
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path	string	true	"orderId"
// @Param			body	body	amendOrderRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/orders/{orderId} [patch]
func (h *Handler) AmendOrderAdmin(w http.ResponseWriter, r *http.Request) {
	var req amendOrderRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	amend := domain.AmendOrderRequest{
		RecipientFirstName: req.RecipientFirstName, RecipientLastName: req.RecipientLastName, RecipientPhone: req.RecipientPhone,
		GiftMessage: req.GiftMessage, DeliveryInstructions: req.DeliveryInstructions,
		DeliveryStreet: req.DeliveryStreet, DeliveryCity: req.DeliveryCity, DeliveryState: req.DeliveryState, DeliveryPostcode: req.DeliveryPostcode,
		BusinessName: req.BusinessName, DeliveryAddressType: req.DeliveryAddressType,
		Reason: req.Reason, MarkAmended: req.MarkAmended,
	}
	// Either half on its own. Requiring both here meant "move it to Friday,
	// same time" was accepted and silently dropped before it ever reached the
	// service — which is where the other half of this fix lives.
	if req.ScheduledDate != nil {
		d, err := timex.ParseDate(*req.ScheduledDate)
		if err != nil {
			httpx.WriteError(w, r, apperr.Validation("scheduledDate must be YYYY-MM-DD."))
			return
		}
		amend.ScheduledDate = &d
	}
	if req.SlotStart != nil {
		amend.SlotStart = req.SlotStart
	}

	order, err := h.svc.AmendOrderAdmin(r.Context(), staffClaims(r), chi.URLParam(r, "orderId"), amend)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, orderDTO(order))
}

// @Summary		Cancel an order
// @Tags			admin-orders
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path	string	true	"orderId"
// @Param			body	body	cancelOrderRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/orders/{orderId}/cancel [post]
func (h *Handler) CancelOrderAdmin(w http.ResponseWriter, r *http.Request) {
	var req cancelOrderRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	r, err := httpx.WithEventTime(r, "statusAt", req.StatusAt)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	outcome, err := h.svc.CancelOrderAdmin(r.Context(), staffClaims(r), chi.URLParam(r, "orderId"), req.Reason)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]interface{}{"refundCents": outcome.RefundCents, "refundPct": outcome.RefundPct, "status": "cancelled"})
}

type refundRequest struct {
	AmountCents int64   `json:"amountCents"`
	Reason      string  `json:"reason"`
	StatusAt    *string `json:"statusAt,omitempty" example:"2026-09-16T10:33:52Z"`
}

// @Summary		Refund an order in full or in part
// @Tags			admin-orders
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path	string	true	"orderId"
// @Param			body	body	refundRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/orders/{orderId}/refund [post]
func (h *Handler) RefundOrderAdmin(w http.ResponseWriter, r *http.Request) {
	var req refundRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	r, err := httpx.WithEventTime(r, "statusAt", req.StatusAt)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	outcome, err := h.svc.RefundOrderAdmin(r.Context(), staffClaims(r), chi.URLParam(r, "orderId"), req.AmountCents, req.Reason)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]interface{}{"refundCents": outcome.RefundCents, "status": "succeeded"})
}

type markStatusRequest struct {
	Note     string  `json:"note"`
	StatusAt *string `json:"statusAt,omitempty" example:"2026-09-16T10:33:52Z"`
}

// @Summary		Mark an order collected or handed to the driver
// @Tags			admin-orders
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path	string	true	"orderId"
// @Param			body	body	markStatusRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/orders/{orderId}/mark-fulfilled [post]
func (h *Handler) MarkFulfilledAdmin(w http.ResponseWriter, r *http.Request) {
	var req markStatusRequest
	_ = httpx.DecodeJSON(r, &req)
	r, err := httpx.WithEventTime(r, "statusAt", req.StatusAt)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.svc.MarkFulfilledAdmin(r.Context(), staffClaims(r), chi.URLParam(r, "orderId"), req.Note); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]interface{}{"success": true, "status": ordersapi.StatusFulfilled})
}

type setStatusRequest struct {
	Status   string  `json:"status"`
	Note     string  `json:"note"`
	StatusAt *string `json:"statusAt,omitempty" example:"2026-09-16T10:33:52Z"`
}

// @Summary		Move an order to another status
// @Tags			admin-orders
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path	string	true	"orderId"
// @Param			body	body	setStatusRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/orders/{orderId}/status [post]
func (h *Handler) SetOrderStatusAdmin(w http.ResponseWriter, r *http.Request) {
	var req setStatusRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	r, err := httpx.WithEventTime(r, "statusAt", req.StatusAt)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.svc.SetStatusAdmin(r.Context(), staffClaims(r), chi.URLParam(r, "orderId"), req.Status, req.Note); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]interface{}{"success": true, "status": req.Status})
}

type manualOrderItemJSON struct {
	ProductID      string   `json:"productId"`
	VariantID      *string  `json:"variantId"`
	FlavourIDs     []string `json:"selectedFlavourIds"`
	AddonIDs       []string `json:"selectedAddonIds"`
	PresentationID *string  `json:"selectedPresentationId"`
	WrapColourID   *string  `json:"selectedWrapColourId"`
	Quantity       int      `json:"quantity"`
}

type createManualOrderRequestJSON struct {
	BranchID       string `json:"branchId"`
	FulfilmentType string `json:"fulfilmentType"`
	ScheduledDate  string `json:"scheduledDate"`
	SlotStart      string `json:"slotStart"`
	Recipient      struct {
		FirstName string  `json:"firstName"`
		LastName  string  `json:"lastName"`
		Phone     *string `json:"phone"`
	} `json:"recipient"`
	GiftMessage *string `json:"giftMessage"`
	IsGift      bool    `json:"isGift"`
	Purchaser   struct {
		FirstName string  `json:"firstName"`
		LastName  string  `json:"lastName"`
		Email     string  `json:"email"`
		Phone     *string `json:"phone"`
	} `json:"purchaser"`
	DeliveryStreet       *string               `json:"deliveryStreet"`
	DeliveryCity         *string               `json:"deliveryCity"`
	DeliveryState        *string               `json:"deliveryState"`
	DeliveryPostcode     *string               `json:"deliveryPostcode"`
	DeliveryInstructions *string               `json:"deliveryInstructions"`
	Items                []manualOrderItemJSON `json:"items"`
}

// @Summary		Create an order on a customer's behalf
// @Tags			admin-orders
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body	createManualOrderRequestJSON	true	"Request body"
// @Success		201	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/admin/orders [post]
func (h *Handler) CreateManualOrder(w http.ResponseWriter, r *http.Request) {
	var req createManualOrderRequestJSON
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	items := make([]domain.ManualOrderItemRequest, 0, len(req.Items))
	for _, i := range req.Items {
		items = append(items, domain.ManualOrderItemRequest{
			ProductID: i.ProductID, VariantID: i.VariantID, FlavourIDs: i.FlavourIDs, AddonIDs: i.AddonIDs,
			PresentationID: i.PresentationID, WrapColourID: i.WrapColourID, Quantity: i.Quantity,
		})
	}

	order, err := h.svc.CreateManualOrder(r.Context(), staffClaims(r), domain.CreateManualOrderRequest{
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		BranchID:       req.BranchID, FulfilmentType: req.FulfilmentType, ScheduledDate: req.ScheduledDate, SlotStart: req.SlotStart,
		Recipient:   domain.Recipient{FirstName: req.Recipient.FirstName, LastName: req.Recipient.LastName, Phone: req.Recipient.Phone},
		GiftMessage: req.GiftMessage, IsGift: req.IsGift,
		Purchaser:      domain.Purchaser{FirstName: req.Purchaser.FirstName, LastName: req.Purchaser.LastName, Email: req.Purchaser.Email, Phone: req.Purchaser.Phone},
		DeliveryStreet: req.DeliveryStreet, DeliveryCity: req.DeliveryCity, DeliveryState: req.DeliveryState, DeliveryPostcode: req.DeliveryPostcode,
		DeliveryInstructions: req.DeliveryInstructions, Items: items,
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Created(w, orderDTO(order))
}

// --- Admin: schedule configuration (SCH-01, SCH-02, SCH-05, SCH-09) -----------

// @Summary		Get a branch's slot rules and cut-off times
// @Tags			admin-scheduling
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	path	string	true	"branchId"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/branches/{branchId}/schedule [get]
func (h *Handler) GetScheduleConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.svc.GetScheduleConfig(r.Context(), chi.URLParam(r, "branchId"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, scheduleConfigDTO(cfg))
}

type scheduleConfigRequest struct {
	SlotDurationMins   int    `json:"slotDurationMins"`
	PickupCutoffTime   string `json:"pickupCutoffTime"`
	DeliveryCutoffTime string `json:"deliveryCutoffTime"`
	MaxOrdersPerSlot   int    `json:"maxOrdersPerSlot"`
	DefaultLeadHours   int    `json:"defaultLeadHours"`
	AdvanceDaysMax     int    `json:"advanceDaysMax"`
}

// @Summary		Update a branch's slot rules and cut-off times
// @Tags			admin-scheduling
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	path	string	true	"branchId"
// @Param			body	body	scheduleConfigRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/branches/{branchId}/schedule [patch]
func (h *Handler) UpdateScheduleConfig(w http.ResponseWriter, r *http.Request) {
	var req scheduleConfigRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	cfg, err := h.svc.UpdateScheduleConfig(r.Context(), ordersapi.ScheduleConfig{
		BranchID: chi.URLParam(r, "branchId"), SlotDurationMins: req.SlotDurationMins,
		PickupCutoffTime: req.PickupCutoffTime, DeliveryCutoffTime: req.DeliveryCutoffTime,
		MaxOrdersPerSlot: req.MaxOrdersPerSlot, DefaultLeadHours: req.DefaultLeadHours, AdvanceDaysMax: req.AdvanceDaysMax,
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, scheduleConfigDTO(cfg))
}

// @Summary		List a branch's slots and remaining capacity
// @Tags			admin-scheduling
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	path	string	true	"branchId"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/branches/{branchId}/slots [get]
func (h *Handler) ListSlotsAdmin(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	date, err := timex.ParseDate(q.Get("date"))
	if err != nil {
		httpx.WriteError(w, r, apperr.Validation("date must be YYYY-MM-DD."))
		return
	}
	fulfilmentType := q.Get("fulfilmentType")
	slots, err := h.svc.ListSlots(r.Context(), chi.URLParam(r, "branchId"), date, fulfilmentType)
	if err != nil {
		httpx.WriteError(w, r, apperr.Internal(err))
		return
	}
	out := make([]slotDTOType, 0, len(slots))
	for _, s := range slots {
		out = append(out, slotDTOType{Start: s.SlotStart, End: s.SlotEnd, Remaining: s.Remaining()})
	}
	httpx.OK(w, out)
}

type upliftCapacityRequest struct {
	MaxCapacity int `json:"maxCapacity"`
}

// @Summary		Raise or lower one slot's capacity
// @Tags			admin-scheduling
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	path	string	true	"branchId"
// @Param			slotId	path	string	true	"slotId"
// @Param			body	body	upliftCapacityRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/branches/{branchId}/slots/{slotId} [patch]
func (h *Handler) UpliftSlotCapacity(w http.ResponseWriter, r *http.Request) {
	var req upliftCapacityRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.svc.UpliftSlotCapacity(r.Context(), chi.URLParam(r, "slotId"), req.MaxCapacity); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"success": true})
}

type collectCashRequest struct {
	// When the cash was taken. Optional; defaults to now.
	PaidAt *string `json:"paidAt,omitempty" example:"2026-09-16T10:33:52Z"`
}

// @Summary		Record cash collected against a cash-on-delivery order
// @Description	Settles a COD order at handover. The order's payment status moves from pending to captured and the pending payment row is captured alongside it. Calling it twice is a conflict, not a second collection.
// @Tags			admin-orders
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path	string	true	"orderId"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"NOT_A_CASH_ORDER"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Failure		409	{object}	httpx.Envelope	"CASH_ALREADY_COLLECTED"
// @Param			body	body	collectCashRequest	false	"Optional: paidAt"
// @Router			/admin/orders/{orderId}/collect-cash [post]
func (h *Handler) RecordCashCollectedAdmin(w http.ResponseWriter, r *http.Request) {
	// Optional body: {"paidAt": "..."} for cash taken earlier than it is recorded.
	var req collectCashRequest
	_ = httpx.DecodeJSON(r, &req)
	r, err := httpx.WithEventTime(r, "paidAt", req.PaidAt)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.svc.RecordCashCollected(r.Context(), staffClaims(r), chi.URLParam(r, "orderId")); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]interface{}{"success": true, "paymentStatus": "captured"})
}
