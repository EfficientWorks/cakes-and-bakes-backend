package domain

import (
	"context"
	"errors"
	"math"
	"sort"
	"strings"
	"time"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/realtime"
	"bakehouse/pkg/tenant"
	"bakehouse/pkg/timex"
	"bakehouse/services/franchise/franchiseapi"
	"bakehouse/services/orders/internal/store"
	"bakehouse/services/orders/ordersapi"
)

/*
 * The Kitchen Display.
 *
 * A KDS works for one location: a store (its own orders) or a central kitchen
 * (every store assigned to it). The location comes from the signed-in staff
 * token — a central-kitchen staff member's token names their kitchen, a
 * store-scoped one names the store — so a KDS rarely has to say anything.
 * Brand-wide staff (a super admin setting a tablet up) name it explicitly.
 */

// UseKDS wires the franchise lookups the KDS needs (kitchen → stores, and
// each location's settings). Nil leaves every KDS endpoint answering 503.
func (s *Service) UseKDS(r franchiseapi.KDSReader) { s.kds = r }

// UseRealtime sets the live-update hub (defaults to realtime.Default).
func (s *Service) UseRealtime(h *realtime.Hub) { s.hub = h }

// KDSScopeQuery is how a request names its location.
type KDSScopeQuery struct {
	BranchID         string
	CentralKitchenID string
	// FilterBranchID narrows a central kitchen's view to one of its stores
	// (the KDS "Choose Store" picker).
	FilterBranchID string
}

// KDSScope is the resolved location and the stores it covers.
type KDSScope struct {
	Type             string
	BranchID         string
	CentralKitchenID string
	Name             string
	Timezone         string
	// Every store the location covers.
	AllBranchIDs []string
	// The stores this request reads (AllBranchIDs, or the picked one).
	BranchIDs []string
	Settings  *franchiseapi.KDSSettings
}

func errKDSUnavailable() error {
	return apperr.New("KDS_UNAVAILABLE", apperr.CategoryUpstream, "The Kitchen Display is not configured on this server.")
}

// ResolveKDSScope turns a request into a location the caller may use.
func (s *Service) ResolveKDSScope(ctx context.Context, claims *auth.StaffClaims, q KDSScopeQuery) (*KDSScope, error) {
	if s.kds == nil {
		return nil, errKDSUnavailable()
	}
	if !tenant.HasPermission(claims, tenant.PermKitchenViewOrders) && !tenant.HasPermission(claims, tenant.PermKDSAccess) {
		return nil, apperr.PermissionDenied("You do not have access to the Kitchen Display.")
	}
	branchID, kitchenID := strings.TrimSpace(q.BranchID), strings.TrimSpace(q.CentralKitchenID)
	if branchID != "" && kitchenID != "" {
		return nil, apperr.Validation("Send either branchId or centralKitchenId, not both.")
	}
	if branchID == "" && kitchenID == "" {
		if ks := tenant.CentralKitchenScopes(claims); len(ks) == 1 {
			kitchenID = ks[0]
		} else if len(ks) == 0 {
			branchID = soleBranch(claims)
		}
	}

	scope := &KDSScope{}
	switch {
	case kitchenID != "":
		if !tenant.IsBrandScoped(claims) && !containsString(tenant.CentralKitchenScopes(claims), kitchenID) {
			return nil, apperr.PermissionDenied("You do not have access to this corporate office.")
		}
		ck, err := s.kds.GetCentralKitchen(ctx, kitchenID)
		if err != nil {
			return nil, apperr.NotFound("Corporate office not found.")
		}
		scope.Type, scope.CentralKitchenID, scope.Name, scope.Timezone =
			franchiseapi.LocationCentralKitchen, ck.ID, ck.Name, ck.Timezone
		scope.AllBranchIDs = ck.BranchIDs
	case branchID != "":
		if !tenant.CanAccessBranch(claims, branchID) {
			return nil, apperr.PermissionDenied("You do not have access to this store.")
		}
		b, err := s.branches.GetBranch(ctx, branchID)
		if err != nil {
			return nil, apperr.NotFound("Store not found.")
		}
		scope.Type, scope.BranchID, scope.Name, scope.Timezone = franchiseapi.LocationBranch, b.ID, b.Name, b.Timezone
		scope.AllBranchIDs = []string{b.ID}
	default:
		return nil, apperr.Validation("Say which location: branchId or centralKitchenId.").
			WithFields(apperr.FieldError{Field: "branchId", Code: "REQUIRED", Message: "Required."})
	}

	scope.BranchIDs = scope.AllBranchIDs
	if f := strings.TrimSpace(q.FilterBranchID); f != "" && scope.Type == franchiseapi.LocationCentralKitchen {
		if !containsString(scope.AllBranchIDs, f) {
			return nil, apperr.Validation("That store is not assigned to this corporate office.").
				WithFields(apperr.FieldError{Field: "storeId", Code: "NOT_ASSIGNED", Message: "Not assigned."})
		}
		scope.BranchIDs = []string{f}
	}
	if scope.BranchIDs == nil {
		scope.BranchIDs = []string{}
	}

	settings, err := s.kds.KDSSettingsFor(ctx, franchiseapi.KDSScope{
		Type: scope.Type, BranchID: scope.BranchID, CentralKitchenID: scope.CentralKitchenID})
	if err != nil {
		return nil, apperr.Internal(err)
	}
	scope.Settings = settings
	return scope, nil
}

// kdsMayActOnBranch reports whether a KDS user may change an order of this
// store: store access as usual, or via a central kitchen that serves it.
func (s *Service) kdsMayActOnBranch(ctx context.Context, claims *auth.StaffClaims, branchID string) bool {
	if tenant.CanAccessBranch(claims, branchID) {
		return true
	}
	if s.kds == nil {
		return false
	}
	for _, ck := range tenant.CentralKitchenScopes(claims) {
		ids, err := s.kds.CentralKitchenBranchIDs(ctx, ck)
		if err == nil && containsString(ids, branchID) {
			return true
		}
	}
	return false
}

func soleBranch(claims *auth.StaffClaims) string {
	found := ""
	for _, ra := range claims.RoleAssignments {
		if ra.ScopeType != tenant.ScopeBranch || ra.BranchID == nil || *ra.BranchID == "" {
			continue
		}
		if found != "" && found != *ra.BranchID {
			return ""
		}
		found = *ra.BranchID
	}
	return found
}

func containsString(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func (sc *KDSScope) location() *time.Location {
	if loc, err := time.LoadLocation(sc.Timezone); err == nil && sc.Timezone != "" {
		return loc
	}
	return time.UTC
}

// ================================================================== the board

// KDS board tabs / stages.
const (
	KDSStageActive    = "active"
	KDSStageReady     = "ready"
	KDSStageFulfilled = "fulfilled"
	KDSStageCancelled = "cancelled"
)

// KDSBoardQuery is GET /kds/orders.
type KDSBoardQuery struct {
	Scope           KDSScopeQuery
	CurrentTime     time.Time
	StartDate       *time.Time
	EndDate         *time.Time
	FulfilmentTypes []string
	Statuses        []string
	KOTStatuses     []string
	// all | active | ready | fulfilled | new | amended | cancelled
	View string
	// Apply the "show orders based on pickup/delivery time" setting.
	ApplyAppearTime bool
	IncludeOverdue  bool
	Search          string
}

// KDSOrderView is one order on the board with what the card needs computed.
type KDSOrderView struct {
	ordersapi.KDSOrderRow
	Stage       string
	SlotStartAt *time.Time
	SlotEndAt   *time.Time
	AppearAt    *time.Time
	IsNew       bool
	Amended     bool
}

// KDSCounts are the numbers on the board's filter chips.
type KDSCounts struct {
	All       int `json:"all"`
	Pickup    int `json:"pickup"`
	Delivery  int `json:"delivery"`
	Takeaway  int `json:"takeaway"`
	Ready     int `json:"ready"`
	Fulfilled int `json:"fulfilled"`
	New       int `json:"new"`
	Amended   int `json:"amended"`
	Cancelled int `json:"cancelled"`
}

// KDSBoard is the board response.
type KDSBoard struct {
	Scope       *KDSScope
	CurrentTime time.Time
	StartDate   time.Time
	EndDate     time.Time
	Orders      []KDSOrderView
	Counts      KDSCounts
}

func kdsStage(o *ordersapi.Order) string {
	switch {
	case o.Status == ordersapi.StatusCancelled || o.KOTStatus == ordersapi.KOTCancelled:
		return KDSStageCancelled
	case ordersapi.KOTHandedOff(o.KOTStatus) || o.Status == ordersapi.StatusFulfilled:
		return KDSStageFulfilled
	case o.KOTStatus == ordersapi.KOTReady || o.KOTStatus == ordersapi.KOTPacking || o.KOTStatus == ordersapi.KOTReadyForDelivery:
		return KDSStageReady
	}
	return KDSStageActive
}

// slotTimes turns an order's branch-local slot into absolute times.
func slotTimes(o *ordersapi.KDSOrderRow, fallback *time.Location) (start, end *time.Time) {
	loc := fallback
	if o.BranchTimezone != "" {
		if l, err := time.LoadLocation(o.BranchTimezone); err == nil {
			loc = l
		}
	}
	parse := func(hhmm string) *time.Time {
		hhmm = strings.TrimSpace(hhmm)
		if len(hhmm) < 5 {
			return nil
		}
		t, err := time.Parse("15:04", hhmm[:5])
		if err != nil {
			return nil
		}
		d := o.ScheduledDate
		v := time.Date(d.Year(), d.Month(), d.Day(), t.Hour(), t.Minute(), 0, 0, loc)
		return &v
	}
	return parse(o.SlotStart), parse(o.SlotEnd)
}

/*
 * GetKDSBoard is the board.
 *
 * The appear-time rule (setting "show orders based on pickup or delivery
 * time"): a ticket still in the queue appears at
 *
 *     slot start − (order appear time + the longest item preparation time)
 *
 * e.g. slot 12:00, appear time 1 h, lines of 15–30 min and 30–60 min → the
 * longest preparation is 60 min, so the ticket appears at 10:00. The current
 * time comes from the request, so a KDS in another timezone, or a test, sees
 * exactly what it asks for. Tickets the kitchen has touched always show.
 */
func (s *Service) GetKDSBoard(ctx context.Context, claims *auth.StaffClaims, q KDSBoardQuery) (*KDSBoard, error) {
	scope, err := s.ResolveKDSScope(ctx, claims, q.Scope)
	if err != nil {
		return nil, err
	}
	loc := scope.location()
	now := q.CurrentTime
	if now.IsZero() {
		now = time.Now()
	}
	today := time.Date(now.In(loc).Year(), now.In(loc).Month(), now.In(loc).Day(), 0, 0, 0, 0, time.UTC)
	from, to := today, today
	if q.StartDate != nil {
		from = *q.StartDate
	}
	if q.EndDate != nil {
		to = *q.EndDate
	}
	if to.Before(from) {
		return nil, apperr.Validation("endDate cannot be before startDate.").
			WithFields(apperr.FieldError{Field: "endDate", Code: "INVALID", Message: "Before startDate."})
	}

	// Fulfilment types: what the request asks for, within what the location's
	// settings show.
	st := scope.Settings
	enabled := map[string]bool{
		ordersapi.FulfilmentPickup:   st.ShowPickup,
		ordersapi.FulfilmentDelivery: st.ShowDelivery,
		ordersapi.FulfilmentTakeaway: st.ShowTakeaway,
	}
	var types []string
	if len(q.FulfilmentTypes) > 0 {
		for _, t := range q.FulfilmentTypes {
			if enabled[t] {
				types = append(types, t)
			}
		}
		if len(types) == 0 {
			types = []string{"__none__"}
		}
	} else {
		for _, t := range []string{ordersapi.FulfilmentPickup, ordersapi.FulfilmentDelivery, ordersapi.FulfilmentTakeaway} {
			if enabled[t] {
				types = append(types, t)
			}
		}
		if len(types) == 0 {
			types = []string{"__none__"}
		}
	}

	rows, err := s.store.ListKDSOrders(ctx, ordersapi.KDSOrderFilter{
		BranchIDs: scope.BranchIDs, FulfilmentTypes: types, Statuses: q.Statuses, KOTStatuses: q.KOTStatuses,
		DateFrom: &from, DateTo: &to, IncludeOverdue: q.IncludeOverdue, Search: q.Search,
	})
	if err != nil {
		return nil, apperr.Internal(err)
	}

	board := &KDSBoard{Scope: scope, CurrentTime: now, StartDate: from, EndDate: to, Orders: []KDSOrderView{}}
	view := strings.ToLower(strings.TrimSpace(q.View))
	for i := range rows {
		r := rows[i]
		v := KDSOrderView{KDSOrderRow: r, Stage: kdsStage(&r.Order)}
		v.SlotStartAt, v.SlotEndAt = slotTimes(&r, loc)
		if v.SlotStartAt != nil {
			appear := v.SlotStartAt.Add(-time.Duration(st.OrderAppearMinutes+r.MaxPrepMinutes) * time.Minute)
			v.AppearAt = &appear
		}
		v.IsNew = v.Stage == KDSStageActive && r.KOTStatus == ordersapi.KOTQueue
		v.Amended = r.IsAmended && !r.AmendedAcknowledged

		// The board goes by date and status only: today's orders, overdue
		// ones and later-dated orders the till started. Prep and slot times
		// no longer hold a ticket back; AppearAt is still sent for display.

		switch v.Stage {
		case KDSStageActive:
			board.Counts.All++
			switch r.FulfilmentType {
			case ordersapi.FulfilmentPickup:
				board.Counts.Pickup++
			case ordersapi.FulfilmentDelivery:
				board.Counts.Delivery++
			case ordersapi.FulfilmentTakeaway:
				board.Counts.Takeaway++
			}
			if v.IsNew {
				board.Counts.New++
			}
		case KDSStageReady:
			board.Counts.Ready++
		case KDSStageFulfilled:
			board.Counts.Fulfilled++
		case KDSStageCancelled:
			board.Counts.Cancelled++
		}
		if v.Amended && (v.Stage == KDSStageActive || v.Stage == KDSStageReady) {
			board.Counts.Amended++
		}

		keep := true
		switch view {
		case "", "all":
		case KDSStageActive:
			// The active rail shows cancelled tickets too, labelled, so a
			// cancellation is seen rather than silently vanishing.
			keep = v.Stage == KDSStageActive || v.Stage == KDSStageCancelled
		case KDSStageReady, KDSStageFulfilled, KDSStageCancelled:
			keep = v.Stage == view
		case "new":
			keep = v.IsNew
		case "amended":
			keep = v.Amended
		}
		if keep {
			board.Orders = append(board.Orders, v)
		}
	}
	return board, nil
}

// GetKDSOrder returns one order with its kitchen timeline.
func (s *Service) GetKDSOrder(ctx context.Context, claims *auth.StaffClaims, orderID string) (*KDSOrderView, []ordersapi.KOTEvent, error) {
	row, err := s.store.GetKDSOrder(ctx, orderID)
	if err != nil {
		return nil, nil, notFoundOrInternal(err, "Order not found.")
	}
	if !s.kdsMayActOnBranch(ctx, claims, row.BranchID) {
		return nil, nil, apperr.NotFound("Order not found.")
	}
	events, err := s.store.ListKOTEvents(ctx, orderID)
	if err != nil {
		return nil, nil, apperr.Internal(err)
	}
	v := &KDSOrderView{KDSOrderRow: *row, Stage: kdsStage(&row.Order)}
	v.SlotStartAt, v.SlotEndAt = slotTimes(row, time.UTC)
	v.IsNew = v.Stage == KDSStageActive && row.KOTStatus == ordersapi.KOTQueue
	v.Amended = row.IsAmended && !row.AmendedAcknowledged
	return v, events, nil
}

// ================================================================== KOT moves

func kotTargetForAction(action, from, fulfilment string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case ordersapi.KDSActionStart:
		return ordersapi.KOTPreparing, nil
	case ordersapi.KDSActionMarkReady, "ready":
		return ordersapi.KOTReady, nil
	case ordersapi.KDSActionPacking:
		return ordersapi.KOTPacking, nil
	case ordersapi.KDSActionReadyForDl:
		return ordersapi.KOTReadyForDelivery, nil
	case ordersapi.KDSActionHandedOver, "handover", "hand_over":
		if fulfilment == ordersapi.FulfilmentDelivery {
			return ordersapi.KOTOutForDelivery, nil
		}
		return ordersapi.KOTCompleted, nil
	case ordersapi.KDSActionDelivered:
		return ordersapi.KOTDelivered, nil
	case ordersapi.KDSActionRecall:
		return ordersapi.KOTReady, nil
	case ordersapi.KDSActionUndo:
		switch from {
		case ordersapi.KOTPreparing:
			return ordersapi.KOTQueue, nil
		case ordersapi.KOTReady:
			return ordersapi.KOTPreparing, nil
		}
		return "", apperr.New("INVALID_KOT_TRANSITION", apperr.CategoryBusinessRule, "Nothing to undo from "+from+".")
	}
	return "", apperr.Validation("Unknown action. Use start, mark_ready, packing, ready_for_delivery, handed_over, delivered, recall or undo.").
		WithFields(apperr.FieldError{Field: "action", Code: "INVALID", Message: "Unknown action."})
}

// KDSOrderKOTResult is the outcome of a KOT move.
type KDSOrderKOTResult struct {
	Order          *KDSOrderView
	FromKOTStatus  string
	ToKOTStatus    string
	OrderStatus    string
	OrderFulfilled bool
}

/*
 * UpdateOrderKOT moves an order through the kitchen and keeps the order
 * status in step, through the same SetStatus path the admin uses (so the
 * status timeline, "ready" / "delivered" emails and the sales roll-up all
 * happen exactly as they do today):
 *
 *   start (→ preparing)      order → in_production (if not yet in production)
 *   mark_ready (→ ready)     order → ready
 *   handed_over / delivered  paid order → fulfilled; unpaid delivery → out_for_delivery;
 *                            unpaid pickup/takeaway stays ready and is fulfilled by the POS payment
 *   delivered                refused until the payment is complete
 *   recall (→ ready)         kitchen status only; the order status is left alone
 */
func (s *Service) UpdateOrderKOT(ctx context.Context, claims *auth.StaffClaims, orderID, action, kotStatus, note string) (*KDSOrderKOTResult, error) {
	if !tenant.HasPermission(claims, tenant.PermKitchenMarkStatus) {
		return nil, apperr.PermissionDenied("You do not have permission to update kitchen status.")
	}
	o, err := s.store.GetOrderByID(ctx, orderID)
	if err != nil {
		return nil, notFoundOrInternal(err, "Order not found.")
	}
	if !s.kdsMayActOnBranch(ctx, claims, o.BranchID) {
		return nil, apperr.NotFound("Order not found.")
	}
	if o.Status == ordersapi.StatusCancelled || o.KOTStatus == ordersapi.KOTCancelled {
		return nil, apperr.New("ORDER_CANCELLED", apperr.CategoryBusinessRule, "This order was cancelled. Do not prepare it.")
	}
	if o.Status == ordersapi.StatusReturned || o.Status == ordersapi.StatusRefunded {
		return nil, apperr.New("ORDER_CLOSED", apperr.CategoryBusinessRule, "This order is closed.")
	}

	from := o.KOTStatus
	var to string
	switch {
	case strings.TrimSpace(action) != "":
		to, err = kotTargetForAction(action, from, o.FulfilmentType)
		if err != nil {
			return nil, err
		}
	case strings.TrimSpace(kotStatus) != "":
		to = ordersapi.NormalizeKOTStatus(kotStatus)
		if !ordersapi.IsValidKOTStatus(to) || to == ordersapi.KOTCancelled {
			return nil, apperr.Validation("\"" + kotStatus + "\" is not a kitchen status the KDS can set. Valid: " +
				strings.Join(ordersapi.AllKOTStatuses()[:8], ", ") + ".").
				WithFields(apperr.FieldError{Field: "kotStatus", Code: "INVALID", Message: "Not a KOT status."})
		}
	default:
		return nil, apperr.Validation("Send an action or a kotStatus.").
			WithFields(apperr.FieldError{Field: "action", Code: "REQUIRED", Message: "Required."})
	}
	if from == to {
		return nil, apperr.New("INVALID_KOT_TRANSITION", apperr.CategoryBusinessRule, "The order is already "+to+".")
	}
	if !ordersapi.CanMoveKOT(from, to) {
		return nil, apperr.New("INVALID_KOT_TRANSITION", apperr.CategoryBusinessRule,
			"An order cannot go from "+from+" to "+to+". From "+from+" it can go to: "+
				strings.Join(ordersapi.NextKOTStatuses(from), ", ")+".").
			WithMeta(map[string]any{"from": from, "to": to, "allowed": ordersapi.NextKOTStatuses(from)})
	}
	paid := o.PaymentStatus == ordersapi.PaymentCaptured
	if to == ordersapi.KOTDelivered && !paid {
		return nil, apperr.New("PAYMENT_PENDING", apperr.CategoryBusinessRule,
			"Take the payment before marking this order delivered.")
	}

	actor := store.KOTActor{ID: staffIDPtr(claims), Type: "staff", Source: "kds", Note: note}
	if err := s.store.MoveOrderKOT(ctx, orderID, from, to, actor); err != nil {
		if errors.Is(err, store.ErrKOTConflict) {
			return nil, apperr.New("KOT_STATUS_CHANGED", apperr.CategoryConflict,
				"This order was updated on another screen. Refresh and try again.")
		}
		return nil, apperr.Internal(err)
	}

	// Keep the order status in step.
	actorID := claims.StaffID
	target := ""
	switch to {
	case ordersapi.KOTPreparing:
		if ordersapi.IsAmendable(o.Status) {
			target = ordersapi.StatusInProduction
		}
	case ordersapi.KOTQueue:
		if o.Status == ordersapi.StatusInProduction {
			target = ordersapi.StatusConfirmed
		}
	case ordersapi.KOTReady:
		if from == ordersapi.KOTPreparing || from == ordersapi.KOTQueue {
			if ordersapi.IsAmendable(o.Status) || o.Status == ordersapi.StatusInProduction {
				target = ordersapi.StatusReady
			}
		}
	case ordersapi.KOTOutForDelivery, ordersapi.KOTDelivered, ordersapi.KOTCompleted:
		switch {
		case paid:
			if o.Status != ordersapi.StatusFulfilled {
				target = ordersapi.StatusFulfilled
			}
		case o.FulfilmentType == ordersapi.FulfilmentDelivery:
			if o.Status != ordersapi.StatusOutForDelivery {
				target = ordersapi.StatusOutForDelivery
			}
		default:
			if ordersapi.IsAmendable(o.Status) || o.Status == ordersapi.StatusInProduction {
				target = ordersapi.StatusReady
			}
		}
	}
	if target != "" {
		msg := "Kitchen: " + strings.ReplaceAll(to, "_", " ")
		if err := s.SetStatus(ctx, actorID, orderID, target, msg); err != nil {
			return nil, err
		}
	}
	/*
	 * Recall reopens the order too. A paid order (every website order) was
	 * fulfilled when it was handed over, and the board files a fulfilled
	 * order as done whatever its kitchen status, so recalling only the
	 * kitchen status left it there. Written straight to the status, not
	 * through SetStatus: a recall is not a fresh "ready", so no ready email
	 * and no second courier.
	 */
	if to == ordersapi.KOTReady && ordersapi.KOTHandedOff(from) &&
		(o.Status == ordersapi.StatusFulfilled || o.Status == ordersapi.StatusOutForDelivery) {
		if _, err := s.store.UpdateOrderStatus(ctx, orderID, ordersapi.StatusReady, "staff", &actorID, "Kitchen: recalled"); err != nil {
			return nil, apperr.Internal(err)
		}
		s.publishOrderByID(ctx, realtime.EventOrderStatusChanged, orderID)
	}

	view, _, err := s.GetKDSOrder(ctx, claims, orderID)
	if err != nil {
		return nil, err
	}
	s.publishOrder(realtime.EventOrderKOTChanged, &view.Order, "")
	return &KDSOrderKOTResult{
		Order: view, FromKOTStatus: from, ToKOTStatus: to,
		OrderStatus: view.Status, OrderFulfilled: view.Status == ordersapi.StatusFulfilled,
	}, nil
}

// KDSItemKOTResult is the outcome of a line move.
type KDSItemKOTResult struct {
	Order         *KDSOrderView
	ItemID        string
	FromKOTStatus string
	ToKOTStatus   string
	OrderStarted  bool
	AllItemsReady bool
}

// UpdateItemKOT moves one line: start (queue → preparing), mark_ready
// (preparing → ready), undo. Starting the first line starts the order.
func (s *Service) UpdateItemKOT(ctx context.Context, claims *auth.StaffClaims, orderID, itemID, action, kotStatus, note string) (*KDSItemKOTResult, error) {
	if !tenant.HasPermission(claims, tenant.PermKitchenMarkStatus) {
		return nil, apperr.PermissionDenied("You do not have permission to update kitchen status.")
	}
	o, err := s.store.GetOrderByID(ctx, orderID)
	if err != nil {
		return nil, notFoundOrInternal(err, "Order not found.")
	}
	if !s.kdsMayActOnBranch(ctx, claims, o.BranchID) {
		return nil, apperr.NotFound("Order not found.")
	}
	if o.Status == ordersapi.StatusCancelled || o.KOTStatus == ordersapi.KOTCancelled {
		return nil, apperr.New("ORDER_CANCELLED", apperr.CategoryBusinessRule, "This order was cancelled. Do not prepare it.")
	}
	if ordersapi.KOTHandedOff(o.KOTStatus) {
		return nil, apperr.New("ORDER_HANDED_OVER", apperr.CategoryBusinessRule, "This order has already left the kitchen.")
	}
	var item *ordersapi.OrderItem
	for i := range o.Items {
		if o.Items[i].ID == itemID {
			item = &o.Items[i]
		}
	}
	if item == nil {
		return nil, apperr.NotFound("Order item not found.")
	}
	if item.KOTStatus == ordersapi.ItemKOTCancelled ||
		item.ProductionStatus == ordersapi.ItemCancelled || item.ProductionStatus == ordersapi.ItemCancel {
		return nil, apperr.New("ITEM_CANCELLED", apperr.CategoryBusinessRule, "This item was cancelled. Do not prepare it.")
	}

	from := item.KOTStatus
	var to string
	switch a := strings.ToLower(strings.TrimSpace(action)); {
	case a == ordersapi.KDSActionStart:
		to = ordersapi.ItemKOTPreparing
	case a == ordersapi.KDSActionMarkReady || a == "ready":
		to = ordersapi.ItemKOTReady
	case a == ordersapi.KDSActionUndo:
		switch from {
		case ordersapi.ItemKOTPreparing:
			to = ordersapi.ItemKOTQueue
		case ordersapi.ItemKOTReady:
			to = ordersapi.ItemKOTPreparing
		}
	case a != "":
		return nil, apperr.Validation("Unknown action. Use start, mark_ready or undo.").
			WithFields(apperr.FieldError{Field: "action", Code: "INVALID", Message: "Unknown action."})
	case strings.TrimSpace(kotStatus) != "":
		to = ordersapi.NormalizeKOTStatus(kotStatus)
		if !ordersapi.IsValidItemKOTStatus(to) || to == ordersapi.ItemKOTCancelled {
			return nil, apperr.Validation("\"" + kotStatus + "\" is not an item kitchen status. Valid: queue, preparing, ready, completed.").
				WithFields(apperr.FieldError{Field: "kotStatus", Code: "INVALID", Message: "Not an item KOT status."})
		}
	default:
		return nil, apperr.Validation("Send an action or a kotStatus.").
			WithFields(apperr.FieldError{Field: "action", Code: "REQUIRED", Message: "Required."})
	}
	if to == "" || !ordersapi.CanMoveItemKOT(from, to) {
		return nil, apperr.New("INVALID_KOT_TRANSITION", apperr.CategoryBusinessRule,
			"An item cannot go from "+from+" to "+to+". From "+from+" it can go to: "+
				strings.Join(ordersapi.NextItemKOTStatuses(from), ", ")+".").
			WithMeta(map[string]any{"from": from, "to": to, "allowed": ordersapi.NextItemKOTStatuses(from)})
	}

	actor := store.KOTActor{ID: staffIDPtr(claims), Type: "staff", Source: "kds", Note: note}
	started, err := s.store.MoveItemKOT(ctx, orderID, itemID, from, to, actor)
	if err != nil {
		if errors.Is(err, store.ErrKOTConflict) {
			return nil, apperr.New("KOT_STATUS_CHANGED", apperr.CategoryConflict,
				"This item was updated on another screen. Refresh and try again.")
		}
		return nil, apperr.Internal(err)
	}
	if started && ordersapi.IsAmendable(o.Status) {
		if err := s.SetStatus(ctx, claims.StaffID, orderID, ordersapi.StatusInProduction, "Kitchen: preparing"); err != nil {
			return nil, err
		}
	}

	view, _, err := s.GetKDSOrder(ctx, claims, orderID)
	if err != nil {
		return nil, err
	}
	allReady := true
	for _, it := range view.Items {
		if it.KOTStatus == ordersapi.ItemKOTCancelled || it.ProductionStatus == ordersapi.ItemCancelled || it.ProductionStatus == ordersapi.ItemCancel {
			continue
		}
		if it.KOTStatus != ordersapi.ItemKOTReady && it.KOTStatus != ordersapi.ItemKOTCompleted {
			allReady = false
		}
	}

	/*
	 * The last item ready makes the order ready.
	 *
	 * Readying items one by one used to leave the order "preparing" until
	 * someone also pressed the order's Mark Ready: the ticket stayed on the
	 * active rail, the customer was never told, and a delivery was never sent
	 * to the courier (which happens when the order becomes ready).
	 */
	if allReady && to == ordersapi.ItemKOTReady && view.KOTStatus == ordersapi.KOTPreparing {
		readyActor := actor
		readyActor.Note = "All items ready"
		if err := s.store.MoveOrderKOT(ctx, orderID, ordersapi.KOTPreparing, ordersapi.KOTReady, readyActor); err == nil {
			if current, err := s.store.GetOrderByID(ctx, orderID); err == nil &&
				(ordersapi.IsAmendable(current.Status) || current.Status == ordersapi.StatusInProduction) {
				if err := s.SetStatus(ctx, claims.StaffID, orderID, ordersapi.StatusReady, "Kitchen: ready"); err != nil {
					return nil, err
				}
			}
			if refreshed, _, err := s.GetKDSOrder(ctx, claims, orderID); err == nil {
				view = refreshed
			}
			s.publishOrder(realtime.EventOrderKOTChanged, &view.Order, "")
		} else if !errors.Is(err, store.ErrKOTConflict) {
			return nil, apperr.Internal(err)
		}
	}
	s.publishOrder(realtime.EventOrderItemKOT, &view.Order, itemID)
	return &KDSItemKOTResult{Order: view, ItemID: itemID, FromKOTStatus: from, ToKOTStatus: to,
		OrderStarted: started, AllItemsReady: allReady}, nil
}

// AcknowledgeAmendment clears an order's Amended flag on the KDS.
func (s *Service) AcknowledgeAmendment(ctx context.Context, claims *auth.StaffClaims, orderID string) (*KDSOrderView, error) {
	o, err := s.store.GetOrderByID(ctx, orderID)
	if err != nil {
		return nil, notFoundOrInternal(err, "Order not found.")
	}
	if !s.kdsMayActOnBranch(ctx, claims, o.BranchID) {
		return nil, apperr.NotFound("Order not found.")
	}
	if !o.IsAmended || o.AmendedAcknowledged {
		return nil, apperr.New("NOTHING_TO_ACKNOWLEDGE", apperr.CategoryBusinessRule, "This order has no unacknowledged amendment.")
	}
	if _, err := s.store.AcknowledgeAmendment(ctx, orderID, staffIDPtr(claims)); err != nil {
		return nil, apperr.Internal(err)
	}
	view, _, err := s.GetKDSOrder(ctx, claims, orderID)
	if err != nil {
		return nil, err
	}
	s.publishOrder(realtime.EventOrderUpdated, &view.Order, "")
	return view, nil
}

// ================================================================== planning

func roundPct(part, whole int) float64 {
	if whole <= 0 {
		return 0
	}
	return math.Round(float64(part)*1000/float64(whole)) / 10
}

func planningRange(scope *KDSScope, now time.Time, start, end *time.Time, defaultDays int) (time.Time, time.Time, error) {
	loc := scope.location()
	n := now.In(loc)
	first := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1) // tomorrow
	from, to := first, first.AddDate(0, 0, defaultDays-1)
	if start != nil {
		from = *start
		if end == nil {
			to = from.AddDate(0, 0, defaultDays-1)
		}
	}
	if end != nil {
		to = *end
	}
	if to.Before(from) {
		return from, to, apperr.Validation("endDate cannot be before startDate.").
			WithFields(apperr.FieldError{Field: "endDate", Code: "INVALID", Message: "Before startDate."})
	}
	if to.Sub(from) > 92*24*time.Hour {
		return from, to, apperr.Validation("A range is at most 93 days.").
			WithFields(apperr.FieldError{Field: "endDate", Code: "RANGE", Message: "At most 93 days."})
	}
	return from, to, nil
}

// KDSSlotSummaryRow is one time slot's share of the orders.
type KDSSlotSummaryRow struct {
	SlotStart  string  `json:"slotStart"`
	SlotEnd    string  `json:"slotEnd"`
	OrderCount int     `json:"orderCount"`
	Percentage float64 `json:"percentage"`
	IsPeak     bool    `json:"isPeak"`
}

// KDSSlotSummary is GET /kds/orders/tomorrow/slots.
type KDSSlotSummary struct {
	StartDate   string              `json:"startDate"`
	EndDate     string              `json:"endDate"`
	TotalOrders int                 `json:"totalOrders"`
	Slots       []KDSSlotSummaryRow `json:"slots"`
}

// PlanningQuery is shared by the tomorrow / week / calendar reads.
type PlanningQuery struct {
	Scope           KDSScopeQuery
	CurrentTime     time.Time
	StartDate       *time.Time
	EndDate         *time.Time
	SlotStart       string
	SlotEnd         string
	FulfilmentTypes []string
}

// KDSSlotSummary groups the range's orders by time slot. Percentage is the
// slot's share of all the orders in the range; the busiest slot is the peak.
func (s *Service) KDSSlotSummary(ctx context.Context, claims *auth.StaffClaims, q PlanningQuery) (*KDSSlotSummary, error) {
	scope, err := s.ResolveKDSScope(ctx, claims, q.Scope)
	if err != nil {
		return nil, err
	}
	from, to, err := planningRange(scope, q.CurrentTime, q.StartDate, q.EndDate, 1)
	if err != nil {
		return nil, err
	}
	counts, err := s.store.KDSSlotCounts(ctx, scope.BranchIDs, from, to, q.FulfilmentTypes)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	type key struct{ start, end string }
	sum := map[key]int{}
	var keys []key
	total, peak := 0, 0
	for _, c := range counts {
		k := key{hhmm(c.SlotStart), hhmm(c.SlotEnd)}
		if _, ok := sum[k]; !ok {
			keys = append(keys, k)
		}
		sum[k] += c.OrderCount
		total += c.OrderCount
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].start < keys[j].start })
	for _, k := range keys {
		if sum[k] > peak {
			peak = sum[k]
		}
	}
	out := &KDSSlotSummary{StartDate: timex.FormatDate(from), EndDate: timex.FormatDate(to), TotalOrders: total, Slots: []KDSSlotSummaryRow{}}
	for _, k := range keys {
		out.Slots = append(out.Slots, KDSSlotSummaryRow{
			SlotStart: k.start, SlotEnd: k.end, OrderCount: sum[k],
			Percentage: roundPct(sum[k], total), IsPeak: peak > 0 && sum[k] == peak,
		})
	}
	return out, nil
}

func hhmm(t string) string {
	if len(t) >= 5 {
		return t[:5]
	}
	return t
}

// KDSProductRow is one product (and variation) to make.
type KDSProductRow struct {
	ProductID     string             `json:"productId"`
	ProductName   string             `json:"productName"`
	VariantName   string             `json:"variantName"`
	TotalQuantity int                `json:"totalQuantity"`
	OrderCount    int                `json:"orderCount"`
	SlotCount     int                `json:"slotCount"`
	Percentage    float64            `json:"percentage"`
	Modifiers     []KDSModifierCount `json:"modifiers"`
	// This row's configuration: the selected variations (flavours such as
	// "15 Cupcakes") and add-ons. The same product with a different set is
	// its own row.
	Variations []string `json:"variations"`
	Addons     []string `json:"addons"`
}

// KDSModifierCount is a flavour or add-on with the units that carry it.
type KDSModifierCount struct {
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
}

// KDSCategoryRow groups products under their category.
type KDSCategoryRow struct {
	CategoryID    string          `json:"categoryId"`
	CategoryName  string          `json:"categoryName"`
	TotalQuantity int             `json:"totalQuantity"`
	OrderCount    int             `json:"orderCount"`
	Products      []KDSProductRow `json:"products"`
}

// KDSProductSummary is GET /kds/orders/tomorrow/products.
type KDSProductSummary struct {
	StartDate     string           `json:"startDate"`
	EndDate       string           `json:"endDate"`
	SlotStart     string           `json:"slotStart,omitempty"`
	SlotEnd       string           `json:"slotEnd,omitempty"`
	TotalQuantity int              `json:"totalQuantity"`
	Categories    []KDSCategoryRow `json:"categories"`
}

// KDSProductSummary groups the range's order lines by category, product and
// variation (optionally for one slot). Percentage is the product's share of
// all units in the range.
func (s *Service) KDSProductSummary(ctx context.Context, claims *auth.StaffClaims, q PlanningQuery) (*KDSProductSummary, error) {
	scope, err := s.ResolveKDSScope(ctx, claims, q.Scope)
	if err != nil {
		return nil, err
	}
	from, to, err := planningRange(scope, q.CurrentTime, q.StartDate, q.EndDate, 1)
	if err != nil {
		return nil, err
	}
	aggs, err := s.store.KDSItemAggregates(ctx, scope.BranchIDs, from, to, q.SlotStart, q.SlotEnd, q.FulfilmentTypes)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	total := 0
	for _, a := range aggs {
		total += a.TotalQuantity
	}
	byCat := map[string]*KDSCategoryRow{}
	var catOrder []string
	catOrders := map[string]int{}
	for _, a := range aggs {
		c, ok := byCat[a.CategoryID]
		if !ok {
			c = &KDSCategoryRow{CategoryID: a.CategoryID, CategoryName: a.CategoryName, Products: []KDSProductRow{}}
			byCat[a.CategoryID] = c
			catOrder = append(catOrder, a.CategoryID)
		}
		mods := make([]KDSModifierCount, 0, len(a.Modifiers))
		for name, qty := range a.Modifiers {
			mods = append(mods, KDSModifierCount{Name: name, Quantity: qty})
		}
		sort.Slice(mods, func(i, j int) bool {
			if mods[i].Quantity != mods[j].Quantity {
				return mods[i].Quantity > mods[j].Quantity
			}
			return mods[i].Name < mods[j].Name
		})
		c.Products = append(c.Products, KDSProductRow{
			ProductID: a.ProductID, ProductName: a.ProductName, VariantName: a.VariantName,
			TotalQuantity: a.TotalQuantity, OrderCount: a.OrderCount, SlotCount: a.SlotCount,
			Percentage: roundPct(a.TotalQuantity, total), Modifiers: mods,
			Variations: nonNilStrings(a.Variations), Addons: nonNilStrings(a.Addons),
		})
		c.TotalQuantity += a.TotalQuantity
		catOrders[a.CategoryID] += a.OrderCount
	}
	out := &KDSProductSummary{StartDate: timex.FormatDate(from), EndDate: timex.FormatDate(to),
		SlotStart: q.SlotStart, SlotEnd: q.SlotEnd, TotalQuantity: total, Categories: []KDSCategoryRow{}}
	sort.SliceStable(catOrder, func(i, j int) bool { return byCat[catOrder[i]].CategoryName < byCat[catOrder[j]].CategoryName })
	for _, id := range catOrder {
		c := byCat[id]
		sort.SliceStable(c.Products, func(i, j int) bool { return c.Products[i].TotalQuantity > c.Products[j].TotalQuantity })
		c.OrderCount = catOrders[id]
		out.Categories = append(out.Categories, *c)
	}
	return out, nil
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// KDSDayRow is one day of the week view.
type KDSDayRow struct {
	Date       string `json:"date"`
	DayName    string `json:"dayName"`
	OrderCount int    `json:"orderCount"`
	// Share of all orders in the range.
	Percentage float64 `json:"percentage"`
	// Relative to the busiest day (100 = the busiest) — for the load bar.
	LoadPercentage float64 `json:"loadPercentage"`
	IsPeak         bool    `json:"isPeak"`
}

// KDSWeekSummary is GET /kds/orders/week.
type KDSWeekSummary struct {
	StartDate     string      `json:"startDate"`
	EndDate       string      `json:"endDate"`
	TotalOrders   int         `json:"totalOrders"`
	AveragePerDay float64     `json:"averagePerDay"`
	PeakDate      *string     `json:"peakDate"`
	Days          []KDSDayRow `json:"days"`
}

// KDSWeekSummary counts orders per day over the range (default: the next 7
// days from tomorrow). Every day in the range is listed, zeros included.
func (s *Service) KDSWeekSummary(ctx context.Context, claims *auth.StaffClaims, q PlanningQuery) (*KDSWeekSummary, error) {
	scope, err := s.ResolveKDSScope(ctx, claims, q.Scope)
	if err != nil {
		return nil, err
	}
	from, to, err := planningRange(scope, q.CurrentTime, q.StartDate, q.EndDate, 7)
	if err != nil {
		return nil, err
	}
	counts, err := s.store.KDSDayCounts(ctx, scope.BranchIDs, from, to, q.FulfilmentTypes)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	byDay := map[string]int{}
	total, peak := 0, 0
	for _, c := range counts {
		d := timex.FormatDate(c.Date)
		byDay[d] += c.OrderCount
		total += c.OrderCount
	}
	for _, n := range byDay {
		if n > peak {
			peak = n
		}
	}
	out := &KDSWeekSummary{StartDate: timex.FormatDate(from), EndDate: timex.FormatDate(to), TotalOrders: total, Days: []KDSDayRow{}}
	days := 0
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		days++
		key := timex.FormatDate(d)
		n := byDay[key]
		row := KDSDayRow{Date: key, DayName: d.Weekday().String(), OrderCount: n,
			Percentage: roundPct(n, total), LoadPercentage: roundPct(n, peak), IsPeak: peak > 0 && n == peak}
		if row.IsPeak && out.PeakDate == nil {
			k := key
			out.PeakDate = &k
		}
		out.Days = append(out.Days, row)
	}
	if days > 0 {
		out.AveragePerDay = math.Round(float64(total)*10/float64(days)) / 10
	}
	return out, nil
}

// KDSCalendarCell is one day × slot.
type KDSCalendarCell struct {
	Date       string `json:"date"`
	SlotStart  string `json:"slotStart"`
	SlotEnd    string `json:"slotEnd"`
	OrderCount int    `json:"orderCount"`
	// none | low | medium | high, from the location's calendar settings.
	Level string `json:"level"`
}

// KDSCalendarLevel is one legend entry.
type KDSCalendarLevel struct {
	Level string `json:"level"`
	From  int    `json:"from"`
	To    *int   `json:"to"`
}

// KDSCalendar is GET /kds/orders/calendar.
type KDSCalendar struct {
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
	Days      []struct {
		Date    string `json:"date"`
		DayName string `json:"dayName"`
		IsToday bool   `json:"isToday"`
	} `json:"days"`
	Slots []struct {
		SlotStart string `json:"slotStart"`
		SlotEnd   string `json:"slotEnd"`
	} `json:"slots"`
	Cells   []KDSCalendarCell  `json:"cells"`
	Levels  []KDSCalendarLevel `json:"levels"`
	Summary struct {
		PreOrders          int     `json:"preOrders"`
		BusiestDay         *string `json:"busiestDay"`
		BusiestDate        *string `json:"busiestDate"`
		AvgPreOrdersPerDay float64 `json:"avgPreOrdersPerDay"`
		BookedSlots        int     `json:"bookedSlots"`
	} `json:"summary"`
}

func calendarLevel(n int, st *franchiseapi.KDSSettings) string {
	switch {
	case n <= 0:
		return "none"
	case n >= st.CalendarHighFrom:
		return "high"
	case n >= st.CalendarMediumFrom:
		return "medium"
	case n >= st.CalendarLowFrom:
		return "low"
	}
	return "none"
}

// KDSCalendar is a day × time-slot grid of order counts with Low / Medium /
// High labels from the location's calendar settings (default: this week,
// Sunday to Saturday).
func (s *Service) KDSCalendar(ctx context.Context, claims *auth.StaffClaims, q PlanningQuery) (*KDSCalendar, error) {
	scope, err := s.ResolveKDSScope(ctx, claims, q.Scope)
	if err != nil {
		return nil, err
	}
	loc := scope.location()
	n := q.CurrentTime.In(loc)
	today := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
	if q.StartDate == nil && q.EndDate == nil {
		start := today.AddDate(0, 0, -int(today.Weekday()))
		end := start.AddDate(0, 0, 6)
		q.StartDate, q.EndDate = &start, &end
	}
	from, to, err := planningRange(scope, q.CurrentTime, q.StartDate, q.EndDate, 7)
	if err != nil {
		return nil, err
	}
	counts, err := s.store.KDSSlotCounts(ctx, scope.BranchIDs, from, to, q.FulfilmentTypes)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	st := scope.Settings
	out := &KDSCalendar{StartDate: timex.FormatDate(from), EndDate: timex.FormatDate(to), Cells: []KDSCalendarCell{}}
	hi := st.CalendarHighFrom
	lowTo, medTo := st.CalendarLowTo, st.CalendarMediumTo
	out.Levels = []KDSCalendarLevel{
		{Level: "none", From: 0, To: new(int)},
		{Level: "low", From: st.CalendarLowFrom, To: &lowTo},
		{Level: "medium", From: st.CalendarMediumFrom, To: &medTo},
		{Level: "high", From: hi, To: nil},
	}

	type slotKey struct{ start, end string }
	slotSeen := map[slotKey]bool{}
	var slots []slotKey
	cell := map[string]int{}
	perDay := map[string]int{}
	for _, c := range counts {
		k := slotKey{hhmm(c.SlotStart), hhmm(c.SlotEnd)}
		if !slotSeen[k] {
			slotSeen[k] = true
			slots = append(slots, k)
		}
		d := timex.FormatDate(c.Date)
		cell[d+"|"+k.start+"|"+k.end] += c.OrderCount
		perDay[d] += c.OrderCount
		out.Summary.PreOrders += c.OrderCount
	}
	sort.Slice(slots, func(i, j int) bool { return slots[i].start < slots[j].start })
	for _, k := range slots {
		out.Slots = append(out.Slots, struct {
			SlotStart string `json:"slotStart"`
			SlotEnd   string `json:"slotEnd"`
		}{k.start, k.end})
	}
	days, busiest := 0, 0
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		days++
		key := timex.FormatDate(d)
		out.Days = append(out.Days, struct {
			Date    string `json:"date"`
			DayName string `json:"dayName"`
			IsToday bool   `json:"isToday"`
		}{key, d.Weekday().String(), d.Equal(today)})
		if perDay[key] > busiest {
			busiest = perDay[key]
			name, date := d.Weekday().String(), key
			out.Summary.BusiestDay, out.Summary.BusiestDate = &name, &date
		}
		for _, k := range slots {
			nOrders := cell[key+"|"+k.start+"|"+k.end]
			if nOrders > 0 {
				out.Summary.BookedSlots++
			}
			out.Cells = append(out.Cells, KDSCalendarCell{Date: key, SlotStart: k.start, SlotEnd: k.end,
				OrderCount: nOrders, Level: calendarLevel(nOrders, st)})
		}
	}
	if out.Slots == nil {
		out.Slots = []struct {
			SlotStart string `json:"slotStart"`
			SlotEnd   string `json:"slotEnd"`
		}{}
	}
	if days > 0 {
		out.Summary.AvgPreOrdersPerDay = math.Round(float64(out.Summary.PreOrders)*10/float64(days)) / 10
	}
	return out, nil
}

// KDSFooter is the board's status line.
type KDSFooter struct {
	PreordersTomorrow int    `json:"preordersTomorrow"`
	SlotsNearlyFull   int    `json:"slotsNearlyFull"`
	TomorrowDate      string `json:"tomorrowDate"`
}

// KDSFooterSummary: tomorrow's pre-orders and how many of tomorrow's bookable
// slots are at 75%+ of capacity.
func (s *Service) KDSFooterSummary(ctx context.Context, claims *auth.StaffClaims, q PlanningQuery) (*KDSFooter, error) {
	scope, err := s.ResolveKDSScope(ctx, claims, q.Scope)
	if err != nil {
		return nil, err
	}
	from, _, err := planningRange(scope, q.CurrentTime, nil, nil, 1)
	if err != nil {
		return nil, err
	}
	days, err := s.store.KDSDayCounts(ctx, scope.BranchIDs, from, from, nil)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	out := &KDSFooter{TomorrowDate: timex.FormatDate(from)}
	for _, d := range days {
		out.PreordersTomorrow += d.OrderCount
	}
	if n, err := s.store.KDSSlotsNearlyFull(ctx, scope.BranchIDs, from); err == nil {
		out.SlotsNearlyFull = n
	}
	return out, nil
}

// ================================================================== live

// publishOrder tells live screens an order changed. Best effort.
func (s *Service) publishOrder(eventType string, o *ordersapi.Order, itemID string) {
	if o == nil {
		return
	}
	h := s.hub
	if h == nil {
		h = realtime.Default
	}
	e := realtime.Event{
		Type: eventType, OrderID: o.ID, OrderItemID: itemID, BranchID: o.BranchID,
		Reference: o.Reference, Status: o.Status, KOTStatus: o.KOTStatus,
	}
	if o.CustomerID != nil {
		e.CustomerID = *o.CustomerID
	}
	// What a till needs to announce the order without fetching it.
	e.Data = map[string]any{
		"fulfilmentType": o.FulfilmentType, "channel": o.Channel,
		"customerName": orderCustomerName(o), "totalCents": o.TotalCents,
	}
	if o.BillNo != nil {
		e.Data["billNo"] = *o.BillNo
	}
	h.Publish(e)
}

// orderCustomerName is who the order is for, as a till shows it.
func orderCustomerName(o *ordersapi.Order) string {
	if n := strings.TrimSpace(o.PurchaserFirstName + " " + o.PurchaserLastName); n != "" {
		return n
	}
	if o.GuestCustomerName != nil && strings.TrimSpace(*o.GuestCustomerName) != "" {
		return strings.TrimSpace(*o.GuestCustomerName)
	}
	return strings.TrimSpace(o.RecipientFirstName + " " + o.RecipientLastName)
}

// publishOrderByID loads and publishes; used by the existing POS/admin paths.
func (s *Service) publishOrderByID(ctx context.Context, eventType, orderID string) {
	if o, err := s.store.GetOrderByID(ctx, orderID); err == nil {
		s.publishOrder(eventType, o, "")
	}
}

// SubscribeKDS opens a live feed for the stores a KDS scope covers.
func (s *Service) SubscribeKDS(scope *KDSScope) (<-chan realtime.Event, func()) {
	h := s.hub
	if h == nil {
		h = realtime.Default
	}
	ids := scope.BranchIDs
	if len(ids) == 0 {
		// A kitchen with no stores listens to nothing rather than everything.
		ids = []string{"__none__"}
	}
	return h.Subscribe(ids)
}
