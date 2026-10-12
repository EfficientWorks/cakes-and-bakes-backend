package api

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/httpx"
	"bakehouse/pkg/timex"
	"bakehouse/services/orders/internal/domain"
	"bakehouse/services/orders/ordersapi"
)

/*
 * The Kitchen Display's order endpoints, under /v1/kds.
 *
 * Every read is scoped to the signed-in location: a store's KDS sees that
 * store; a central kitchen's KDS sees every store assigned to it (narrowed with
 * ?storeId= from its store picker). Brand-wide staff name the location with
 * ?branchId= or ?centralKitchenId=.
 */

// applyKDSListFilters adds the KDS filters to the existing order lists:
// kotStatus, and startDate / endDate as aliases of dateFrom / dateTo.
func applyKDSListFilters(q url.Values, f *ordersapi.ListOrdersFilter) error {
	if v := strings.TrimSpace(q.Get("kotStatus")); v != "" && v != "all" {
		parts := []string{}
		for _, p := range strings.Split(v, ",") {
			n := ordersapi.NormalizeKOTStatus(p)
			if n == "" {
				continue
			}
			if !ordersapi.IsValidKOTStatus(n) {
				return apperr.Validation("kotStatus must be one of: " + strings.Join(ordersapi.AllKOTStatuses(), ", ") + ".").
					WithFields(apperr.FieldError{Field: "kotStatus", Code: "INVALID", Message: "Unknown kitchen status."})
			}
			parts = append(parts, n)
		}
		f.KOTStatus = strings.Join(parts, ",")
	}
	if v := q.Get("startDate"); v != "" && f.DateFrom == nil {
		t, err := timex.ParseDate(v)
		if err != nil {
			return apperr.Validation("startDate must be YYYY-MM-DD.")
		}
		f.DateFrom = &t
	}
	if v := q.Get("endDate"); v != "" && f.DateTo == nil {
		t, err := timex.ParseDate(v)
		if err != nil {
			return apperr.Validation("endDate must be YYYY-MM-DD.")
		}
		f.DateTo = &t
	}
	return nil
}

func kdsScopeQuery(q url.Values) domain.KDSScopeQuery {
	return domain.KDSScopeQuery{
		BranchID: q.Get("branchId"), CentralKitchenID: q.Get("centralKitchenId"),
		FilterBranchID: q.Get("storeId"),
	}
}

func parseDateParam(q url.Values, name string) (*time.Time, error) {
	v := strings.TrimSpace(q.Get(name))
	if v == "" {
		return nil, nil
	}
	t, err := timex.ParseDate(v)
	if err != nil {
		return nil, apperr.Validation(name + " must be YYYY-MM-DD.").
			WithFields(apperr.FieldError{Field: name, Code: "INVALID", Message: "YYYY-MM-DD."})
	}
	return &t, nil
}

func parseCurrentTime(q url.Values) (time.Time, error) {
	raw := strings.TrimSpace(q.Get("currentTime"))
	if raw == "" {
		return time.Now(), nil
	}
	t, ok := timex.ParseEventTime(&raw)
	if !ok || t == nil {
		return time.Time{}, apperr.Validation("currentTime must be an RFC3339 date-time, e.g. 2026-09-26T10:00:00-04:00.").
			WithFields(apperr.FieldError{Field: "currentTime", Code: "INVALID", Message: "Not a date-time."})
	}
	return *t, nil
}

func csvParam(q url.Values, name string, normalize func(string) string) []string {
	var out []string
	for _, p := range strings.Split(q.Get(name), ",") {
		v := strings.ToLower(strings.TrimSpace(p))
		if normalize != nil {
			v = normalize(v)
		}
		if v != "" && v != "all" {
			out = append(out, v)
		}
	}
	return out
}

func boolParam(q url.Values, name string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(q.Get(name))) {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	}
	return def
}

// ---------------------------------------------------------------- DTOs

type kdsItemDTO struct {
	orderItemDTOType
	CategoryID      string `json:"categoryId"`
	CategoryName    string `json:"categoryName"`
	MinPrepTime     int    `json:"minPrepTime"`
	MinPrepTimeUnit string `json:"minPrepTimeUnit"`
	MaxPrepTime     int    `json:"maxPrepTime"`
	MaxPrepTimeUnit string `json:"maxPrepTimeUnit"`
	MinPrepMinutes  int    `json:"minPrepMinutes"`
	MaxPrepMinutes  int    `json:"maxPrepMinutes"`
}

// kdsOrderDTO is one KDS ticket.
type kdsOrderDTO struct {
	ID             string  `json:"id"`
	Reference      string  `json:"reference"`
	OrderNo        *int    `json:"orderNo"`
	BranchID       string  `json:"branchId"`
	BranchName     string  `json:"branchName"`
	Channel        string  `json:"channel"`
	FulfilmentType string  `json:"fulfilmentType"`
	Status         string  `json:"status"`
	PaymentStatus  string  `json:"paymentStatus"`
	PaymentMethod  string  `json:"paymentMethod"`
	KotStatus      string  `json:"kotStatus"`
	KotStartedAt   *string `json:"kotStartedAt"`
	KotReadyAt     *string `json:"kotReadyAt"`
	KotCompletedAt *string `json:"kotCompletedAt"`
	// active | ready | fulfilled | cancelled — which rail the ticket sits on.
	Stage string `json:"stage"`
	IsNew bool   `json:"isNew"`

	ScheduledDate string  `json:"scheduledDate"`
	SlotStart     string  `json:"slotStart"`
	SlotEnd       string  `json:"slotEnd"`
	SlotStartAt   *string `json:"slotStartAt"`
	SlotEndAt     *string `json:"slotEndAt"`
	// When the ticket appears on the board (slot start − appear time − longest prep).
	AppearAt *string `json:"appearAt"`
	// Across the lines: shortest minimum and longest maximum prep, in minutes.
	MinPrepMinutes int `json:"minPrepMinutes"`
	MaxPrepMinutes int `json:"maxPrepMinutes"`

	Recipient            recipientNameDTOType `json:"recipient"`
	Purchaser            purchaserDTOType     `json:"purchaser"`
	IsGift               bool                 `json:"isGift"`
	GiftMessage          *string              `json:"giftMessage"`
	GiftReason           *string              `json:"giftReason"`
	Notes                *string              `json:"notes"`
	DeliveryAddress      *addressDTOType      `json:"deliveryAddress"`
	DeliveryInstructions *string              `json:"deliveryInstructions"`
	GuestCustomerName    *string              `json:"guestCustomerName"`

	// Recalled by the kitchen after it was handed over (back on the ready
	// rail); false again once it is handed over.
	IsRecalled bool    `json:"isRecalled"`
	RecalledAt *string `json:"recalledAt"`

	IsAmended             bool    `json:"isAmended"`
	AmendedAt             *string `json:"amendedAt"`
	AmendedAcknowledged   bool    `json:"amendedAcknowledged"`
	AmendedAcknowledgedAt *string `json:"amendedAcknowledgedAt"`

	// Always null since courier integration was removed; kept for the KDS
	// response contract.
	Delivery *deliveryDTOType `json:"delivery"`

	Items     []kdsItemDTO `json:"items"`
	CreatedAt string       `json:"createdAt"`
	UpdatedAt string       `json:"updatedAt"`
}

func kdsOrder(v *domain.KDSOrderView) kdsOrderDTO {
	o := &v.Order
	out := kdsOrderDTO{
		ID: o.ID, Reference: o.Reference, OrderNo: o.BillNo, BranchID: o.BranchID, BranchName: v.BranchName,
		Channel: o.Channel, FulfilmentType: o.FulfilmentType, Status: o.Status,
		PaymentStatus: o.PaymentStatus, PaymentMethod: o.PaymentMethod,
		KotStatus: orDefaultStr(o.KOTStatus, ordersapi.KOTQueue), KotStartedAt: fmtTimePtr(o.KOTStartedAt),
		KotReadyAt: fmtTimePtr(o.KOTReadyAt), KotCompletedAt: fmtTimePtr(o.KOTCompletedAt),
		Stage: v.Stage, IsNew: v.IsNew,
		ScheduledDate: o.ScheduledDate.Format(dateFmt), SlotStart: o.SlotStart, SlotEnd: o.SlotEnd,
		SlotStartAt: fmtTimePtr(v.SlotStartAt), SlotEndAt: fmtTimePtr(v.SlotEndAt), AppearAt: fmtTimePtr(v.AppearAt),
		MinPrepMinutes: v.MinPrepMinutes, MaxPrepMinutes: v.MaxPrepMinutes,
		Recipient: recipientNameDTOType{
			FirstName: o.RecipientFirstName, LastName: o.RecipientLastName, Phone: o.RecipientPhone, Email: o.RecipientEmail,
			BusinessName: o.BusinessName, DeliveryAddressType: o.DeliveryAddressType,
			DeliveryInstructions: o.DeliveryInstructions, DeliveryPostcode: o.DeliveryPostcode,
			DeliveryState: o.DeliveryState, DeliveryCity: o.DeliveryCity, DeliveryStreet: o.DeliveryStreet,
			Address: o.DeliveryStreet, City: o.DeliveryCity, Postcode: o.DeliveryPostcode,
		},
		Purchaser: purchaserDTOType{FirstName: o.PurchaserFirstName, LastName: o.PurchaserLastName, Email: o.PurchaserEmail, Phone: o.PurchaserPhone},
		IsGift:    o.IsGift, GiftMessage: o.GiftMessage, GiftReason: o.GiftReason, Notes: o.Notes,
		DeliveryInstructions: o.DeliveryInstructions, GuestCustomerName: o.GuestCustomerName,
		IsAmended: o.IsAmended, AmendedAt: fmtTimePtr(o.AmendedAt),
		AmendedAcknowledged: o.AmendedAcknowledged, AmendedAcknowledgedAt: fmtTimePtr(o.AmendedAcknowledgedAt),
		IsRecalled: v.RecalledAt != nil, RecalledAt: fmtTimePtr(v.RecalledAt),
		Items:     []kdsItemDTO{},
		CreatedAt: o.CreatedAt.Format(rfc3339), UpdatedAt: o.UpdatedAt.Format(rfc3339),
	}
	if o.DeliveryStreet != nil {
		out.DeliveryAddress = &addressDTOType{Street: o.DeliveryStreet, City: o.DeliveryCity, State: o.DeliveryState, Postcode: o.DeliveryPostcode}
	}
	for i := range o.Items {
		it := &o.Items[i]
		out.Items = append(out.Items, kdsItemDTO{
			orderItemDTOType: orderItemDTO(it),
			CategoryID:       it.CategoryID, CategoryName: it.CategoryName,
			MinPrepTime: it.MinPrepTime, MinPrepTimeUnit: it.MinPrepTimeUnit,
			MaxPrepTime: it.MaxPrepTime, MaxPrepTimeUnit: it.MaxPrepTimeUnit,
			MinPrepMinutes: it.MinPrepMinutes, MaxPrepMinutes: it.MaxPrepMinutes,
		})
	}
	return out
}

type kdsScopeDTO struct {
	Type             string   `json:"type"`
	BranchID         string   `json:"branchId"`
	CentralKitchenID string   `json:"centralKitchenId"`
	Name             string   `json:"name"`
	Timezone         string   `json:"timezone"`
	BranchIDs        []string `json:"branchIds"`
	SettingsID       string   `json:"settingsId"`
}

func kdsScope(sc *domain.KDSScope) kdsScopeDTO {
	out := kdsScopeDTO{Type: sc.Type, BranchID: sc.BranchID, CentralKitchenID: sc.CentralKitchenID,
		Name: sc.Name, Timezone: sc.Timezone, BranchIDs: sc.BranchIDs}
	if sc.Settings != nil {
		out.SettingsID = sc.Settings.ID
	}
	if out.BranchIDs == nil {
		out.BranchIDs = []string{}
	}
	return out
}

type kdsBoardDTO struct {
	Scope       kdsScopeDTO      `json:"scope"`
	CurrentTime string           `json:"currentTime"`
	StartDate   string           `json:"startDate"`
	EndDate     string           `json:"endDate"`
	Counts      domain.KDSCounts `json:"counts"`
	Orders      []kdsOrderDTO    `json:"orders"`
}

type kotEventDTO struct {
	ID          string  `json:"id"`
	OrderItemID *string `json:"orderItemId"`
	FromStatus  *string `json:"fromStatus"`
	ToStatus    string  `json:"toStatus"`
	ActorID     *string `json:"actorId"`
	ActorType   string  `json:"actorType"`
	Source      string  `json:"source"`
	Note        *string `json:"note"`
	At          string  `json:"at"`
}

// ---------------------------------------------------------------- board

// @Summary		KDS board: orders for the signed-in location
// @Description	Orders the kitchen should see, with counts for the filter chips. Queued tickets only appear once currentTime ≥ slot start − (orderAppearMinutes + the longest item max prep time), when the location's "show orders based on time" setting is on. Fulfilment types are limited by the location's showPickup / showDelivery / showTakeaway settings.
// @Tags			kds
// @Produce		json
// @Security		BearerAuth
// @Param			currentTime			query	string	false	"RFC3339 'now' used for the appear-time rule (default: server now)"
// @Param			startDate			query	string	false	"YYYY-MM-DD scheduled date from (default: today in the location's timezone)"
// @Param			endDate				query	string	false	"YYYY-MM-DD scheduled date to (default: startDate)"
// @Param			fulfilmentType		query	string	false	"pickup, delivery, takeaway (comma-separated)"
// @Param			status				query	string	false	"Order status filter (comma-separated)"
// @Param			kotStatus			query	string	false	"Kitchen status filter (comma-separated)"
// @Param			view				query	string	false	"all (default), active, ready, fulfilled, cancelled, new, amended"
// @Param			applyAppearTime		query	bool	false	"Apply the appear-time rule (default true)"
// @Param			includeOverdue		query	bool	false	"Also show unfinished orders from before startDate (default true)"
// @Param			search				query	string	false	"Reference or name"
// @Param			storeId				query	string	false	"Central kitchen: narrow to one assigned store"
// @Param			branchId			query	string	false	"Brand-wide staff: the store"
// @Param			centralKitchenId	query	string	false	"Brand-wide staff: the central kitchen"
// @Success		200	{object}	httpx.Envelope{data=kdsBoardDTO}
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/kds/orders [get]
func (h *Handler) KDSListOrders(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	now, err := parseCurrentTime(q)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	start, err := parseDateParam(q, "startDate")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	end, err := parseDateParam(q, "endDate")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if start != nil && end == nil {
		end = start
	}
	fulfilment := csvParam(q, "fulfilmentType", nil)
	for _, f := range fulfilment {
		if f != ordersapi.FulfilmentPickup && f != ordersapi.FulfilmentDelivery && f != ordersapi.FulfilmentTakeaway {
			httpx.WriteError(w, r, apperr.Validation("fulfilmentType must be pickup, delivery or takeaway."))
			return
		}
	}
	kot := csvParam(q, "kotStatus", ordersapi.NormalizeKOTStatus)
	for _, k := range kot {
		if !ordersapi.IsValidKOTStatus(k) {
			httpx.WriteError(w, r, apperr.Validation("kotStatus must be one of: "+strings.Join(ordersapi.AllKOTStatuses(), ", ")+"."))
			return
		}
	}
	board, err := h.svc.GetKDSBoard(r.Context(), staffClaims(r), domain.KDSBoardQuery{
		Scope: kdsScopeQuery(q), CurrentTime: now, StartDate: start, EndDate: end,
		FulfilmentTypes: fulfilment, Statuses: csvParam(q, "status", ordersapi.NormalizeStatus), KOTStatuses: kot,
		View: q.Get("view"), ApplyAppearTime: boolParam(q, "applyAppearTime", true),
		IncludeOverdue: boolParam(q, "includeOverdue", true), Search: q.Get("search"),
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out := kdsBoardDTO{Scope: kdsScope(board.Scope), CurrentTime: board.CurrentTime.Format(rfc3339),
		StartDate: timex.FormatDate(board.StartDate), EndDate: timex.FormatDate(board.EndDate),
		Counts: board.Counts, Orders: []kdsOrderDTO{}}
	for i := range board.Orders {
		out.Orders = append(out.Orders, kdsOrder(&board.Orders[i]))
	}
	httpx.OK(w, out)
}

// @Summary		KDS: one order with its kitchen timeline
// @Tags			kds
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path	string	true	"orderId"
// @Success		200	{object}	httpx.Envelope
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/kds/orders/{orderId} [get]
func (h *Handler) KDSGetOrder(w http.ResponseWriter, r *http.Request) {
	v, events, err := h.svc.GetKDSOrder(r.Context(), staffClaims(r), chi.URLParam(r, "orderId"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	timeline := make([]kotEventDTO, 0, len(events))
	for _, e := range events {
		timeline = append(timeline, kotEventDTO{ID: e.ID, OrderItemID: e.OrderItemID, FromStatus: e.FromStatus,
			ToStatus: e.ToStatus, ActorID: e.ActorID, ActorType: e.ActorType, Source: e.Source, Note: e.Note,
			At: e.CreatedAt.Format(rfc3339)})
	}
	statusTimeline := make([]timelineEventDTOType, 0, len(v.Timeline))
	for _, e := range v.Timeline {
		statusTimeline = append(statusTimeline, timelineEventDTOType{Status: e.Status, At: e.CreatedAt.Format(rfc3339), Actor: e.ActorType, Note: derefStr(e.Note)})
	}
	httpx.OK(w, map[string]any{"order": kdsOrder(v), "kotTimeline": timeline, "statusTimeline": statusTimeline})
}

// kdsKOTRequest moves an order or an item through the kitchen. Send an action
// (the KDS button) or an explicit kotStatus.
type kdsKOTRequest struct {
	// Order: start, mark_ready, packing, ready_for_delivery, handed_over, delivered, recall, undo.
	// Item: start, mark_ready, undo.
	Action string `json:"action,omitempty" example:"start"`
	// Alternative to action: the target kitchen status.
	KotStatus string `json:"kotStatus,omitempty" example:"preparing"`
	Note      string `json:"note,omitempty"`
	// When it happened (RFC3339). Defaults to now.
	StatusAt *string `json:"statusAt,omitempty" example:"2026-09-26T10:05:00Z"`
}

// @Summary		KDS: move an order through the kitchen
// @Description	start (queue→preparing, order → in_production) · mark_ready (preparing→ready, order → ready) · handed_over (ready→out_for_delivery for delivery, →completed for pickup/takeaway; a paid order becomes fulfilled) · delivered (requires payment captured; order → fulfilled) · packing / ready_for_delivery · recall (handed over → ready) · undo. Writes the kitchen timeline and, where the order status changes, the status timeline.
// @Tags			kds
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path	string			true	"orderId"
// @Param			body	body	kdsKOTRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		409	{object}	httpx.Envelope	"KOT_STATUS_CHANGED"
// @Failure		422	{object}	httpx.Envelope	"INVALID_KOT_TRANSITION, PAYMENT_PENDING, ORDER_CANCELLED"
// @Router			/kds/orders/{orderId}/kot-status [patch]
func (h *Handler) KDSUpdateOrderKOT(w http.ResponseWriter, r *http.Request) {
	var req kdsKOTRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	r, err := httpx.WithEventTime(r, "statusAt", req.StatusAt)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	res, err := h.svc.UpdateOrderKOT(r.Context(), staffClaims(r), chi.URLParam(r, "orderId"), req.Action, req.KotStatus, req.Note)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]any{
		"order": kdsOrder(res.Order), "fromKotStatus": res.FromKOTStatus, "toKotStatus": res.ToKOTStatus,
		"orderStatus": res.OrderStatus, "orderFulfilled": res.OrderFulfilled,
	})
}

// @Summary		KDS: move one item through the kitchen
// @Description	start (queue→preparing; starting the first item also starts the order) · mark_ready (preparing→ready) · undo. Cancelled items cannot move.
// @Tags			kds
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path	string			true	"orderId"
// @Param			itemId	path	string			true	"itemId"
// @Param			body	body	kdsKOTRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		409	{object}	httpx.Envelope	"KOT_STATUS_CHANGED"
// @Failure		422	{object}	httpx.Envelope	"INVALID_KOT_TRANSITION, ITEM_CANCELLED"
// @Router			/kds/orders/{orderId}/items/{itemId}/kot-status [patch]
func (h *Handler) KDSUpdateItemKOT(w http.ResponseWriter, r *http.Request) {
	var req kdsKOTRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	r, err := httpx.WithEventTime(r, "statusAt", req.StatusAt)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	res, err := h.svc.UpdateItemKOT(r.Context(), staffClaims(r), chi.URLParam(r, "orderId"), chi.URLParam(r, "itemId"),
		req.Action, req.KotStatus, req.Note)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]any{
		"order": kdsOrder(res.Order), "itemId": res.ItemID, "fromKotStatus": res.FromKOTStatus,
		"toKotStatus": res.ToKOTStatus, "orderStarted": res.OrderStarted, "allItemsReady": res.AllItemsReady,
	})
}

// @Summary		KDS: acknowledge an amended order
// @Tags			kds
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path	string	true	"orderId"
// @Success		200	{object}	httpx.Envelope
// @Failure		422	{object}	httpx.Envelope	"NOTHING_TO_ACKNOWLEDGE"
// @Router			/kds/orders/{orderId}/acknowledge-amendment [post]
func (h *Handler) KDSAcknowledgeAmendment(w http.ResponseWriter, r *http.Request) {
	v, err := h.svc.AcknowledgeAmendment(r.Context(), staffClaims(r), chi.URLParam(r, "orderId"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]any{"order": kdsOrder(v)})
}

// ---------------------------------------------------------------- planning

func planningQuery(r *http.Request) (domain.PlanningQuery, error) {
	q := r.URL.Query()
	now, err := parseCurrentTime(q)
	if err != nil {
		return domain.PlanningQuery{}, err
	}
	start, err := parseDateParam(q, "startDate")
	if err != nil {
		return domain.PlanningQuery{}, err
	}
	end, err := parseDateParam(q, "endDate")
	if err != nil {
		return domain.PlanningQuery{}, err
	}
	norm := func(t string) (string, error) {
		t = strings.TrimSpace(t)
		if t == "" {
			return "", nil
		}
		if len(t) == 5 {
			t += ":00"
		}
		if _, err := time.Parse("15:04:05", t); err != nil {
			return "", apperr.Validation("Slot times are HH:MM or HH:MM:SS.")
		}
		return t, nil
	}
	slotStart, err := norm(q.Get("slotStart"))
	if err != nil {
		return domain.PlanningQuery{}, err
	}
	slotEnd, err := norm(q.Get("slotEnd"))
	if err != nil {
		return domain.PlanningQuery{}, err
	}
	return domain.PlanningQuery{Scope: kdsScopeQuery(q), CurrentTime: now, StartDate: start, EndDate: end,
		SlotStart: slotStart, SlotEnd: slotEnd, FulfilmentTypes: csvParam(q, "fulfilmentType", nil)}, nil
}

// @Summary		KDS Tomorrow: orders grouped by time slot
// @Description	Order count per slot over the range (default: tomorrow), each slot's percentage of all orders in the range, and the peak slot.
// @Tags			kds-planning
// @Produce		json
// @Security		BearerAuth
// @Param			startDate			query	string	false	"YYYY-MM-DD (default tomorrow)"
// @Param			endDate				query	string	false	"YYYY-MM-DD (default startDate)"
// @Param			currentTime			query	string	false	"RFC3339; decides what 'tomorrow' is"
// @Param			fulfilmentType		query	string	false	"pickup, delivery, takeaway"
// @Param			storeId				query	string	false	"Central kitchen: one assigned store"
// @Param			branchId			query	string	false	"Brand-wide staff: the store"
// @Param			centralKitchenId	query	string	false	"Brand-wide staff: the central kitchen"
// @Success		200	{object}	httpx.Envelope{data=domain.KDSSlotSummary}
// @Router			/kds/orders/tomorrow/slots [get]
func (h *Handler) KDSSlotSummary(w http.ResponseWriter, r *http.Request) {
	q, err := planningQuery(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.svc.KDSSlotSummary(r.Context(), staffClaims(r), q)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, out)
}

// @Summary		KDS Tomorrow: items to make, by category and product
// @Description	Order lines grouped by category → product/variation with total quantity, order count, slot count, percentage of all units, and flavour/add-on counts. Pass slotStart (and slotEnd) for the time-slot drawer.
// @Tags			kds-planning
// @Produce		json
// @Security		BearerAuth
// @Param			startDate			query	string	false	"YYYY-MM-DD (default tomorrow)"
// @Param			endDate				query	string	false	"YYYY-MM-DD (default startDate)"
// @Param			slotStart			query	string	false	"HH:MM — one time slot only"
// @Param			slotEnd				query	string	false	"HH:MM"
// @Param			currentTime			query	string	false	"RFC3339"
// @Param			fulfilmentType		query	string	false	"pickup, delivery, takeaway"
// @Param			storeId				query	string	false	"Central kitchen: one assigned store"
// @Param			branchId			query	string	false	"Brand-wide staff: the store"
// @Param			centralKitchenId	query	string	false	"Brand-wide staff: the central kitchen"
// @Success		200	{object}	httpx.Envelope{data=domain.KDSProductSummary}
// @Router			/kds/orders/tomorrow/products [get]
func (h *Handler) KDSProductSummary(w http.ResponseWriter, r *http.Request) {
	q, err := planningQuery(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.svc.KDSProductSummary(r.Context(), staffClaims(r), q)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, out)
}

// @Summary		KDS Week: orders per day
// @Description	Every day in the range (default: the 7 days from tomorrow) with its order count, share of the range (percentage), load relative to the busiest day (loadPercentage) and isPeak. The day drawer uses GET /kds/orders?startDate=D&endDate=D&applyAppearTime=false.
// @Tags			kds-planning
// @Produce		json
// @Security		BearerAuth
// @Param			startDate			query	string	false	"YYYY-MM-DD (default tomorrow)"
// @Param			endDate				query	string	false	"YYYY-MM-DD (default startDate + 6)"
// @Param			currentTime			query	string	false	"RFC3339"
// @Param			fulfilmentType		query	string	false	"pickup, delivery, takeaway"
// @Param			storeId				query	string	false	"Central kitchen: one assigned store"
// @Param			branchId			query	string	false	"Brand-wide staff: the store"
// @Param			centralKitchenId	query	string	false	"Brand-wide staff: the central kitchen"
// @Success		200	{object}	httpx.Envelope{data=domain.KDSWeekSummary}
// @Router			/kds/orders/week [get]
func (h *Handler) KDSWeekSummary(w http.ResponseWriter, r *http.Request) {
	q, err := planningQuery(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.svc.KDSWeekSummary(r.Context(), staffClaims(r), q)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, out)
}

// @Summary		KDS Calendar: day × time-slot grid
// @Description	Order counts per day and slot with Low / Medium / High levels from the location's calendar settings, plus Pre Orders, Busiest Day, Avg Pre-Orders/Day and Booked Slots. Default range: this week, Sunday–Saturday.
// @Tags			kds-planning
// @Produce		json
// @Security		BearerAuth
// @Param			startDate			query	string	false	"YYYY-MM-DD"
// @Param			endDate				query	string	false	"YYYY-MM-DD"
// @Param			currentTime			query	string	false	"RFC3339"
// @Param			fulfilmentType		query	string	false	"pickup, delivery, takeaway"
// @Param			storeId				query	string	false	"Central kitchen: one assigned store"
// @Param			branchId			query	string	false	"Brand-wide staff: the store"
// @Param			centralKitchenId	query	string	false	"Brand-wide staff: the central kitchen"
// @Success		200	{object}	httpx.Envelope{data=domain.KDSCalendar}
// @Router			/kds/orders/calendar [get]
func (h *Handler) KDSCalendar(w http.ResponseWriter, r *http.Request) {
	q, err := planningQuery(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.svc.KDSCalendar(r.Context(), staffClaims(r), q)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, out)
}

// @Summary		KDS footer: tomorrow's pre-orders and nearly-full slots
// @Tags			kds-planning
// @Produce		json
// @Security		BearerAuth
// @Param			currentTime			query	string	false	"RFC3339"
// @Param			storeId				query	string	false	"Central kitchen: one assigned store"
// @Param			branchId			query	string	false	"Brand-wide staff: the store"
// @Param			centralKitchenId	query	string	false	"Brand-wide staff: the central kitchen"
// @Success		200	{object}	httpx.Envelope{data=domain.KDSFooter}
// @Router			/kds/orders/summary [get]
func (h *Handler) KDSFooterSummary(w http.ResponseWriter, r *http.Request) {
	q, err := planningQuery(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.svc.KDSFooterSummary(r.Context(), staffClaims(r), q)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, out)
}

// ---------------------------------------------------------------- live

// @Summary		KDS live updates (Server-Sent Events)
// @Description	text/event-stream of order changes for the location's stores: order.created, order.updated, order.amended, order.status_changed, order.kot_status_changed, order.item.kot_status_changed, order.cancelled. Each data line is JSON {type, orderId, orderItemId, branchId, reference, status, kotStatus, at}. A browser EventSource may pass the token as ?accessToken=. A ": ping" comment is sent every 25 s.
// @Tags			kds
// @Produce		text/event-stream
// @Security		BearerAuth
// @Param			accessToken			query	string	false	"Access token (EventSource cannot set headers)"
// @Param			storeId				query	string	false	"Central kitchen: one assigned store"
// @Param			branchId			query	string	false	"Brand-wide staff: the store"
// @Param			centralKitchenId	query	string	false	"Brand-wide staff: the central kitchen"
// @Success		200	{string}	string	"event stream"
// @Router			/kds/events [get]
func (h *Handler) KDSEvents(w http.ResponseWriter, r *http.Request) {
	scope, err := h.svc.ResolveKDSScope(r.Context(), staffClaims(r), kdsScopeQuery(r.URL.Query()))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpx.WriteError(w, r, apperr.Internal(fmt.Errorf("streaming unsupported")))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	events, unsubscribe := h.svc.SubscribeKDS(scope)
	defer unsubscribe()

	fmt.Fprintf(w, "retry: 3000\nevent: ready\ndata: {\"branchIds\":%q}\n\n", strings.Join(scope.BranchIDs, ","))
	flusher.Flush()

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case e, open := <-events:
			if !open {
				return
			}
			fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", e.ID, e.Type, e.JSON())
			flusher.Flush()
		}
	}
}
