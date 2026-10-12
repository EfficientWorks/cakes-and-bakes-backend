package ordersapi

import (
	"strings"
	"time"
)

/*
 * Kitchen (KOT) status.
 *
 * A second, independent status next to the order status. The order status is
 * the customer-facing workflow (placed → confirmed → … → fulfilled) that the
 * website, the admin and every existing report read; the KOT status is the
 * kitchen's own view of the same order and its lines, driven by the KDS.
 *
 * They are linked in one direction only, by the KDS actions: starting an order
 * moves it to in_production, marking it ready moves it to ready, and handing it
 * over fulfils it when it is paid. Nothing about an existing order changes
 * until a KDS acts on it — every order starts in "queue".
 */

// Order KOT statuses.
const (
	KOTQueue            = "queue"
	KOTPreparing        = "preparing"
	KOTReady            = "ready"
	KOTPacking          = "packing"
	KOTReadyForDelivery = "ready_for_delivery"
	KOTOutForDelivery   = "out_for_delivery"
	KOTDelivered        = "delivered"
	KOTCompleted        = "completed"
	KOTCancelled        = "cancelled"
)

// Item KOT statuses.
const (
	ItemKOTQueue     = "queue"
	ItemKOTPreparing = "preparing"
	ItemKOTReady     = "ready"
	ItemKOTCompleted = "completed"
	ItemKOTCancelled = "cancelled"
)

// AllKOTStatuses is the order-level vocabulary, in workflow order.
func AllKOTStatuses() []string {
	return []string{KOTQueue, KOTPreparing, KOTReady, KOTPacking, KOTReadyForDelivery,
		KOTOutForDelivery, KOTDelivered, KOTCompleted, KOTCancelled}
}

// AllItemKOTStatuses is the line-level vocabulary.
func AllItemKOTStatuses() []string {
	return []string{ItemKOTQueue, ItemKOTPreparing, ItemKOTReady, ItemKOTCompleted, ItemKOTCancelled}
}

// NormalizeKOTStatus accepts the spellings a client may send.
func NormalizeKOTStatus(s string) string {
	v := strings.ToLower(strings.TrimSpace(s))
	v = strings.NewReplacer("-", "_", " ", "_").Replace(v)
	switch v {
	case "queued", "new", "pending":
		return KOTQueue
	case "preparation", "in_progress", "started", "in_production":
		return KOTPreparing
	case "readyfordelivery":
		return KOTReadyForDelivery
	case "outfordelivery":
		return KOTOutForDelivery
	case "canceled":
		return KOTCancelled
	}
	return v
}

// IsValidKOTStatus reports whether s is an order KOT status.
func IsValidKOTStatus(s string) bool { return contains(AllKOTStatuses(), s) }

// IsValidItemKOTStatus reports whether s is a line KOT status.
func IsValidItemKOTStatus(s string) bool { return contains(AllItemKOTStatuses(), s) }

// KOTInKitchen is true while the kitchen is working on something: from the
// moment it is started until it leaves the kitchen. Used by the POS/admin
// guards that refuse cancelling or changing what is already being made.
func KOTInKitchen(s string) bool {
	switch s {
	case KOTPreparing, KOTReady, KOTPacking, KOTReadyForDelivery:
		return true
	}
	return false
}

// KOTHandedOff is true once the order has left the kitchen.
func KOTHandedOff(s string) bool {
	switch s {
	case KOTOutForDelivery, KOTDelivered, KOTCompleted:
		return true
	}
	return false
}

// kotTransitions is every order-level move the KDS may make.
var kotTransitions = map[string][]string{
	KOTQueue:            {KOTPreparing},
	KOTPreparing:        {KOTReady, KOTQueue},
	KOTReady:            {KOTPacking, KOTReadyForDelivery, KOTOutForDelivery, KOTDelivered, KOTCompleted, KOTPreparing},
	KOTPacking:          {KOTReadyForDelivery, KOTOutForDelivery, KOTDelivered, KOTCompleted, KOTReady},
	KOTReadyForDelivery: {KOTOutForDelivery, KOTDelivered, KOTCompleted, KOTReady},
	// Recall brings a handed-over order back to the ready rail.
	KOTOutForDelivery: {KOTDelivered, KOTReady},
	KOTDelivered:      {KOTReady},
	KOTCompleted:      {KOTReady},
}

// CanMoveKOT reports whether the KDS may move an order from one KOT status to
// another.
func CanMoveKOT(from, to string) bool { return contains(kotTransitions[from], to) }

// NextKOTStatuses lists where an order may go from here, for error messages.
func NextKOTStatuses(from string) []string { return kotTransitions[from] }

// itemTransitions is every line-level move.
var itemTransitions = map[string][]string{
	ItemKOTQueue:     {ItemKOTPreparing},
	ItemKOTPreparing: {ItemKOTReady, ItemKOTQueue},
	ItemKOTReady:     {ItemKOTPreparing, ItemKOTCompleted},
}

// CanMoveItemKOT reports whether a line may move between KOT statuses.
func CanMoveItemKOT(from, to string) bool { return contains(itemTransitions[from], to) }

// NextItemKOTStatuses lists where a line may go from here.
func NextItemKOTStatuses(from string) []string { return itemTransitions[from] }

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

// KDS actions, as the buttons on the KDS name them.
const (
	KDSActionStart      = "start"      // queue → preparing
	KDSActionMarkReady  = "mark_ready" // preparing → ready
	KDSActionPacking    = "packing"    // ready → packing
	KDSActionReadyForDl = "ready_for_delivery"
	KDSActionHandedOver = "handed_over" // ready → out_for_delivery (delivery) / completed (pickup, takeaway)
	KDSActionDelivered  = "delivered"   // → delivered (payment must be complete)
	KDSActionRecall     = "recall"      // handed over → ready
	KDSActionUndo       = "undo"        // one step back (preparing → queue, ready → preparing)
)

// KOTEvent is one entry on the kitchen timeline.
type KOTEvent struct {
	ID          string
	OrderID     string
	OrderItemID *string
	FromStatus  *string
	ToStatus    string
	ActorID     *string
	ActorType   string
	Source      string
	Note        *string
	CreatedAt   time.Time
}

// KDSOrderFilter is what a KDS board asks for.
type KDSOrderFilter struct {
	BranchIDs       []string // the stores in scope (already authorised)
	FulfilmentTypes []string // pickup | delivery | takeaway
	Statuses        []string // order status filter
	KOTStatuses     []string // KOT status filter
	DateFrom        *time.Time
	DateTo          *time.Time
	IncludeOverdue  bool // also active orders scheduled before DateFrom
	Search          string
}

// KDSOrderRow is an order as the KDS board needs it: the order itself plus
// the store it belongs to and the preparation window of its lines.
type KDSOrderRow struct {
	Order
	BranchName     string
	BranchTimezone string
	// Across the non-cancelled lines: the shortest minimum and the longest
	// maximum preparation time, in minutes.
	MinPrepMinutes int
	MaxPrepMinutes int
	// Set when the till started this later-dated order early (POS "Start
	// Process"): it shows on the board now, whatever its appear time.
	ReleasedAt *time.Time
	// Set when the KDS recalled this handed-over order; cleared when it is
	// handed over again.
	RecalledAt *time.Time
}

// KDSSlotCount is one time slot's volume.
type KDSSlotCount struct {
	Date       time.Time
	SlotStart  string
	SlotEnd    string
	OrderCount int
}

// KDSItemAggregate is one product (and variation) across a set of orders.
type KDSItemAggregate struct {
	CategoryID    string
	CategoryName  string
	ProductID     string
	ProductName   string
	VariantName   string
	TotalQuantity int
	OrderCount    int
	SlotCount     int
	// Flavour/add-on names with how many units carry each.
	Modifiers map[string]int
	// The configuration this row is: its selected flavours (variations such
	// as "15 Cupcakes") and add-ons. Lines with a different set are a
	// different row.
	Variations []string
	Addons     []string
}

// KDSDayCount is one day's order volume.
type KDSDayCount struct {
	Date       time.Time
	OrderCount int
}
