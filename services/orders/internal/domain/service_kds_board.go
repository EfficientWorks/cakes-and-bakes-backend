package domain

import (
	"context"
	"strings"
	"time"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/services/orders/ordersapi"
)

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
