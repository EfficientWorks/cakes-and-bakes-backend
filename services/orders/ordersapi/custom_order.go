package ordersapi

import (
	"context"
	"time"
)

// SourceCustomOrder marks an order created from the Custom Orders form.
const SourceCustomOrder = "custom_order"

// Custom order limits.
const (
	// CustomOrderMessageMax is the longest message on a custom cake, in
	// characters.
	CustomOrderMessageMax = 40
	// CustomOrderMaxPhotos is how many photos a custom order may carry.
	CustomOrderMaxPhotos = 3
)

// CustomOrderRequest is a Custom Orders form turned into an order: price to
// be confirmed (£0), unpaid, for the chosen store and date, so it reaches the
// kitchen display and the admin order list like any other order.
type CustomOrderRequest struct {
	EnquiryID      string
	BranchID       string
	FulfilmentType string // pickup (default) | delivery
	Date           time.Time
	// SlotStart and SlotEnd are "HH:MM". Empty start: a 30-minute window from
	// 09:00; empty end: 30 minutes after the start.
	SlotStart string
	SlotEnd   string

	Name  string
	Email string
	Phone string

	// Delivery address (delivery only); Street falls back to the form's
	// event location.
	Street   string
	City     string
	Postcode string

	// What the line says; empty is "Custom cake".
	Title string
	// CakeMessage is at most CustomOrderMessageMax characters.
	CakeMessage string
	// PhotoRefs are customer-photo upload ids or urls; at most
	// CustomOrderMaxPhotos, each one this server stored.
	PhotoRefs []string
	// Notes carries the rest of the form; stored as the line's comments.
	Notes string
}

// CustomOrderCreator is how the enquiries service turns a custom-order
// enquiry into an order without knowing how orders are stored.
type CustomOrderCreator interface {
	// ValidateCustomOrder checks the request without writing anything, so a
	// bad date or photo is refused before the enquiry is recorded.
	ValidateCustomOrder(ctx context.Context, req CustomOrderRequest) error
	// CreateCustomOrder creates the order; idempotent per EnquiryID.
	CreateCustomOrder(ctx context.Context, req CustomOrderRequest) (*Order, error)
}

// PaymentMethodNone stores an order with no payment method yet (an unpaid
// custom order, priced later).
const PaymentMethodNone = "none"

// IsCustomOrder reports whether the order came from the Custom Orders form.
func (o Order) IsCustomOrder() bool { return o.Source != nil && *o.Source == SourceCustomOrder }
