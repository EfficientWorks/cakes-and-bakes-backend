package ordersapi

import "time"

// ListOrdersFilter drives both the admin order list (ADM-01) and the
// customer order history (account/orders).
type ListOrdersFilter struct {
	BranchID        string
	CustomerID      string
	Status          string
	FulfilmentType  string
	PaymentType     string
	Channel         string
	OrderFilterType string
	// KDS: kitchen status, comma-separated (queue, preparing, ready, ...).
	KOTStatus string
	DateFrom  *time.Time
	DateTo    *time.Time
	Search    string // matches reference, purchaser name, recipient name, phone
	// Sort is "recent" (default: newest created first) or "updated" (last
	// touched first).
	Sort  string
	Page  int
	Limit int
}

const (
	OrderFilterTypeActive    = "active"
	OrderFilterTypeTakeaway  = "takeaway"
	OrderFilterTypeDelivery  = "delivery"
	OrderFilterTypePickup    = "pickup"
	OrderFilterTypeOnline    = "online"
	OrderFilterTypeCompleted = "completed"
)

var (
	closedForActive   = []string{StatusScheduled, StatusFulfilled, StatusCancelled, StatusReturned, StatusRefunded}
	completedStatuses = []string{StatusFulfilled, StatusCancelled, StatusReturned, StatusRefunded}
)

func OrderFilterTypeConditions(t string) (channel, fulfilment string, statuses []string, statusIn bool, ok bool) {
	switch t {
	case OrderFilterTypeActive:
		return "", "", closedForActive, false, true
	case OrderFilterTypeTakeaway:
		return "pos", FulfilmentTakeaway, closedForActive, false, true
	// Pickup and Delivery list website orders too, not only the till's.
	case OrderFilterTypeDelivery:
		return "", FulfilmentDelivery, closedForActive, false, true
	case OrderFilterTypePickup:
		return "", FulfilmentPickup, closedForActive, false, true
	case OrderFilterTypeOnline:
		return "web", "", closedForActive, false, true
	case OrderFilterTypeCompleted:
		return "", "", completedStatuses, true, true
	}
	return "", "", nil, false, false
}

func AllOrderFilterTypes() []string {
	return []string{OrderFilterTypeActive, OrderFilterTypeTakeaway, OrderFilterTypeDelivery,
		OrderFilterTypePickup, OrderFilterTypeOnline, OrderFilterTypeCompleted}
}

// OrderListSummary is computed over the whole filtered set, not the page:
// what the list is worth and how much of it is still scheduled.
type OrderListSummary struct {
	TotalOrders int
	// Net: what the matched orders are worth after cancellations and
	// returns. Cancelled, fully returned and refunded orders count nothing;
	// a partly returned order counts what was kept (its total is already
	// recomputed from the kept lines when the return is recorded).
	TotalCents      int64
	ScheduledOrders int

	// What the set was rung up for before anything came off:
	// TotalCents + CancelledCents + ReturnedCents.
	GrossCents int64
	// Cancelled orders in the set and what they were worth.
	CancelledOrders int
	CancelledCents  int64
	// Orders in the set with a return (partial or full), and what was handed
	// back on them: the refunds (gross - discount + tax) plus, for a fully
	// returned or refunded order, any total left on it (delivery, service).
	ReturnedOrders int
	ReturnedCents  int64
}
