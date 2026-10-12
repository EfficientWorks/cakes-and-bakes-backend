// Package realtime fans order changes out to live screens (the KDS) over
// Server-Sent Events.
//
// In-process and best effort by design. A screen that misses an event is not
// wrong for long: every KDS also re-reads its board, and an event only tells
// it that something changed and where. When the API runs as more than one
// instance this is the seam a shared broker (Redis pub/sub) would sit behind —
// Publish and Subscribe are the whole contract.
package realtime

import (
	"encoding/json"
	"sync"
	"time"
)

// Event types the KDS listens for.
const (
	EventOrderCreated       = "order.created"
	EventOrderUpdated       = "order.updated"
	EventOrderStatusChanged = "order.status_changed"
	EventOrderKOTChanged    = "order.kot_status_changed"
	EventOrderItemKOT       = "order.item.kot_status_changed"
	EventOrderAmended       = "order.amended"
	EventOrderCancelled     = "order.cancelled"
)

// Catalogue event types, for the admin console and the website. Branch-less:
// a KDS (subscribed to its stores) never receives them.
const (
	EventCatalogProduct      = "catalog.product.changed"
	EventCatalogCategory     = "catalog.category.changed"
	EventCatalogModifier     = "catalog.modifier.changed"
	EventCatalogCoupon       = "catalog.coupon.changed"
	EventCatalogContent      = "catalog.content.changed"
	EventCatalogAvailability = "catalog.availability.changed"
)

// Event is one change, addressed to the store it belongs to.
type Event struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	OrderID     string `json:"orderId"`
	OrderItemID string `json:"orderItemId,omitempty"`
	BranchID    string `json:"branchId"`
	Reference   string `json:"reference,omitempty"`
	Status      string `json:"status,omitempty"`
	KOTStatus   string `json:"kotStatus,omitempty"`
	// The customer an order belongs to. Used to route order events to that
	// customer's website session; never sent on the wire.
	CustomerID string         `json:"-"`
	Data       map[string]any `json:"data,omitempty"`
	At         time.Time      `json:"at"`
}

// JSON renders the event for the wire.
func (e Event) JSON() []byte {
	b, _ := json.Marshal(e)
	return b
}

type subscriber struct {
	branches map[string]bool // empty = every branch
	// match, when set, decides instead of branches.
	match func(Event) bool
	ch    chan Event
}

// Hub is the process-wide fan-out point.
type Hub struct {
	mu   sync.RWMutex
	subs map[*subscriber]struct{}
	seq  uint64
}

// NewHub returns an empty hub.
func NewHub() *Hub { return &Hub{subs: map[*subscriber]struct{}{}} }

// Default is the hub every service publishes to. One per process.
var Default = NewHub()

// Subscribe registers a listener for the given branches (none = all) and
// returns its channel and an unsubscribe func. The channel is buffered and a
// slow reader drops events rather than blocking publishers.
func (h *Hub) Subscribe(branchIDs []string) (<-chan Event, func()) {
	s := &subscriber{branches: map[string]bool{}, ch: make(chan Event, 64)}
	for _, id := range branchIDs {
		if id != "" {
			s.branches[id] = true
		}
	}
	h.mu.Lock()
	h.subs[s] = struct{}{}
	h.mu.Unlock()
	return s.ch, func() {
		h.mu.Lock()
		if _, ok := h.subs[s]; ok {
			delete(h.subs, s)
			close(s.ch)
		}
		h.mu.Unlock()
	}
}

// SubscribeMatch registers a listener that receives every event match
// accepts. Same buffering and unsubscribe contract as Subscribe.
func (h *Hub) SubscribeMatch(match func(Event) bool) (<-chan Event, func()) {
	s := &subscriber{branches: map[string]bool{}, match: match, ch: make(chan Event, 64)}
	h.mu.Lock()
	h.subs[s] = struct{}{}
	h.mu.Unlock()
	return s.ch, func() {
		h.mu.Lock()
		if _, ok := h.subs[s]; ok {
			delete(h.subs, s)
			close(s.ch)
		}
		h.mu.Unlock()
	}
}

// Publish delivers an event to every interested subscriber without blocking.
func (h *Hub) Publish(e Event) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.seq++
	e.ID = time.Now().UTC().Format("20060102150405") + "-" + itoa(h.seq)
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	h.mu.Unlock()

	h.mu.RLock()
	defer h.mu.RUnlock()
	for s := range h.subs {
		if s.match != nil {
			if !s.match(e) {
				continue
			}
		} else if len(s.branches) > 0 && !s.branches[e.BranchID] {
			continue
		}
		select {
		case s.ch <- e:
		default: // slow consumer: drop, it will re-read the board
		}
	}
}

func itoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
