package ordersapi

import (
	"strings"
	"time"
)

// --- Scheduling (SCH-01 to SCH-09) ------------------------------------------

type ScheduleConfig struct {
	BranchID           string
	SlotDurationMins   int
	PickupCutoffTime   string // "16:00"
	DeliveryCutoffTime string
	MaxOrdersPerSlot   int
	DefaultLeadHours   int
	AdvanceDaysMax     int
}

type SlotCapacity struct {
	ID             string
	BranchID       string
	Date           time.Time
	SlotStart      string
	SlotEnd        string
	FulfilmentType string
	MaxCapacity    int
	ReservedCount  int
}

func (s SlotCapacity) Remaining() int {
	r := s.MaxCapacity - s.ReservedCount
	if r < 0 {
		return 0
	}
	return r
}

type DaySlots struct {
	Date  string
	Label string
	Slots []SlotCapacity
}

// --- Cart --------------------------------------------------------------------

type Cart struct {
	ID               string
	BranchID         string
	CustomerID       *string
	FulfilmentType   string
	DeliveryStreet   *string
	DeliveryCity     *string
	DeliveryState    *string
	DeliveryPostcode *string
	ScheduledDate    *time.Time
	SlotStart        *string
	SlotEnd          *string
	DiscountCode     *string
	TipCents         int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
	// Where the delivery address is, located when it is set and only for a
	// store that prices delivery by radius. Nil otherwise.
	DeliveryLatitude  *float64
	DeliveryLongitude *float64

	Items []CartItem
}

type SelectedModifier struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	PriceDelta int64  `json:"priceDelta"`
}

type CartItemRecipient struct {
	FirstName string
	LastName  string
}

type CartItem struct {
	ID          string
	CartID      string
	ProductID   string
	ProductName string
	// ProductSlug and ProductImageURL are joined from the catalogue on read,
	// not stored on the line: they are how the storefront links a cart row
	// back to its product page and renders its thumbnail, and neither is part
	// of what the customer agreed to. Nullable, because a product deleted out
	// from under a live cart still has to render.
	ProductSlug          *string
	ProductImageURL      *string
	VariantID            *string
	VariantName          *string
	UnitPriceCents       int64
	Quantity             int
	SelectedFlavours     []SelectedModifier
	SelectedAddons       []SelectedModifier
	SelectedPresentation *SelectedModifier
	SelectedWrapColour   *SelectedModifier
	IsGift               bool
	RecipientFirstName   *string
	RecipientLastName    *string
	GiftMessage          *string
	Available            bool
	// Restriction is set when a seasonal tag's restricted dates rule this
	// line out for the cart's chosen slot (website carts).
	Restriction *ItemRestriction
	CreatedAt   time.Time
}

// ItemRestriction says why a cart line can't be ordered for the chosen slot.
type ItemRestriction struct {
	TagID         string
	TagName       string
	WindowFrom    string // store-local "YYYY-MM-DDTHH:MM:SS"
	WindowTo      string
	AvailableFrom string // first free date, "YYYY-MM-DD"
	Message       string
}

func (i CartItem) UnitTotalCents() int64 {
	total := i.UnitPriceCents
	for _, f := range i.SelectedFlavours {
		total += f.PriceDelta
	}
	for _, a := range i.SelectedAddons {
		total += a.PriceDelta
	}
	if i.SelectedPresentation != nil {
		total += i.SelectedPresentation.PriceDelta
	}
	if i.SelectedWrapColour != nil {
		total += i.SelectedWrapColour.PriceDelta
	}
	return total
}

func (i CartItem) LineTotalCents() int64 {
	return i.UnitTotalCents() * int64(i.Quantity)
}

// --- Orders --------------------------------------------------------------------

// Delivery address types. A courier treats a workplace differently from a
// home (reception hours, a company on the door), so the storefront asks.
const (
	AddressTypeResidence = "residence"
	AddressTypeBusiness  = "business"
)

// IsValidAddressType reports whether t is one of the accepted address types.
func IsValidAddressType(t string) bool {
	return t == AddressTypeResidence || t == AddressTypeBusiness
}

type StorefrontRecipient struct {
	ID             string `json:"id"`
	PurchaserEmail string `json:"purchaserEmail"`
	// CustomerID is set when the purchaser was signed in at the time the
	// recipient was saved; nil for a guest purchaser known only by email.
	CustomerID           *string   `json:"customerId,omitempty"`
	RecipientEmail       string    `json:"email"`
	FirstName            string    `json:"firstName"`
	LastName             string    `json:"lastName"`
	Phone                *string   `json:"phone,omitempty"`
	BusinessName         *string   `json:"businessName,omitempty"`
	DeliveryAddressType  *string   `json:"deliveryAddressType,omitempty"`
	DeliveryInstructions *string   `json:"deliveryInstructions,omitempty"`
	DeliveryPostcode     *string   `json:"deliveryPostcode,omitempty"`
	DeliveryState        *string   `json:"deliveryState,omitempty"`
	DeliveryCity         *string   `json:"deliveryCity,omitempty"`
	DeliveryStreet       *string   `json:"deliveryStreet,omitempty"`
	CreatedAt            time.Time `json:"createdAt"`
	UpdatedAt            time.Time `json:"updatedAt"`
}

const (
	StatusPlaced         = "placed"
	StatusConfirmed      = "confirmed"
	StatusScheduled      = "scheduled"
	StatusInProduction   = "in_production"
	StatusReady          = "ready"
	StatusOutForDelivery = "out_for_delivery"
	StatusFulfilled      = "fulfilled"
	StatusCancelled      = "cancelled"
	// The order stood, and the money went back. Distinct from cancelled,
	// which says it never happened.
	StatusRefunded = "refunded"
	// The goods came back over the counter. The database has allowed this
	// since the POS migration and the return flow writes it, but it had no
	// constant here — so code that wanted to reason about a returned order
	// either spelled it as a literal or, more often, forgot it existed.
	StatusReturned = "returned"
)

// AllStatuses is the order workflow, in the sequence it runs.
//
// Kept in one place because three things have to agree on it: this package,
// the CHECK constraint on orders.status, and the console's status filter.
func AllStatuses() []string {
	return []string{
		StatusPlaced, StatusConfirmed, StatusScheduled, StatusInProduction,
		StatusReady, StatusOutForDelivery, StatusFulfilled, StatusCancelled,
	}
}

func NormalizeStatus(status string) string {
	s := strings.ToLower(strings.TrimSpace(status))
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, " ", "_")
	switch s {
	case "canceled":
		return StatusCancelled
	case "completed", "complete", "delivered":
		return StatusFulfilled
	}
	return s
}

// IsValidStatus reports whether a string is one of them.
func IsValidStatus(status string) bool {
	for _, s := range AllStatuses() {
		if s == status {
			return true
		}
	}
	return false
}

const (
	FulfilmentPickup   = "pickup"
	FulfilmentDelivery = "delivery"
	// Ordered and taken away at the counter. Distinct from pickup, which is
	// an order placed ahead and collected later — the reports count them
	// separately because they are different things to staff a shop for.
	FulfilmentTakeaway = "takeaway"

	PaymentPending       = "pending"
	PaymentCaptured      = "captured"
	PaymentFailed        = "failed"
	PaymentRefunded      = "refunded"
	PaymentPartialRefund = "partial_refund"
)

// preProductionStates are the statuses in which an order may still be
// amended or cancelled (Section 13.1: amendable until production starts).
var preProductionStates = map[string]bool{
	StatusPlaced: true, StatusConfirmed: true, StatusScheduled: true,
}

func IsAmendable(status string) bool { return preProductionStates[status] }

type Order struct {
	ID             string
	Reference      string
	BranchID       string
	CustomerID     *string
	Channel        string
	FulfilmentType string

	RecipientFirstName string
	RecipientLastName  string
	RecipientPhone     *string
	// RecipientEmail is the receiver's own address, not the purchaser's:
	// PurchaserEmail is where the receipt goes, this is where a notification
	// to the person receiving the order would go. Usually absent on a gift.
	RecipientEmail *string
	GiftMessage    *string
	// GiftReason is why it is being sent — "Birthday", "Sympathy". Free text,
	// because the list is the storefront's to choose and adding an occasion
	// should not need a migration.
	GiftReason *string
	IsGift     bool

	PurchaserFirstName string
	PurchaserLastName  string
	PurchaserEmail     string
	PurchaserPhone     *string

	DeliveryStreet       *string
	DeliveryCity         *string
	DeliveryState        *string
	DeliveryPostcode     *string
	DeliveryInstructions *string
	// BusinessName is the company on the door when delivering to a workplace;
	// DeliveryAddressType is "residence" or "business" (see AddressType*).
	// Both nil on pickup orders and on orders placed before they were asked.
	BusinessName        *string
	DeliveryAddressType *string
	// The two delivery preferences the checkout offers, kept as flags rather
	// than folded into DeliveryInstructions so despatch can act on them.
	PhotoOnDelivery  bool
	SurpriseDelivery bool

	ScheduledDate  time.Time
	SlotStart      string
	SlotEnd        string
	SlotCapacityID *string

	SubtotalCents    int64
	DeliveryFeeCents int64
	TaxCents         int64
	TipCents         int64
	DiscountCents    int64
	// DiscountCode is the code as typed onto the order, not a reference to a
	// coupon row: the order has to keep reading correctly after the coupon is
	// edited, paused or deleted, the same as every other denormalised field.
	DiscountCode *string
	// The percentage a counter operator chose, kept alongside the amount it
	// came to so a receipt can print what was actually offered.
	DiscountPct *float64
	// Rate the tax was computed at. Nil on orders stored before the rate
	// was kept, where the API falls back to deriving it from the cents.
	TaxPct *float64
	// Charged at the counter, alongside tax and delivery.
	ServiceChargeCents int64
	TotalCents         int64
	Currency           string

	Status            string
	PaymentStatus     string
	ProviderPaymentID *string

	// How the order is being paid for. Denormalised onto the order rather
	// than left only on the payment row: the kitchen ticket, the driver run
	// sheet and the admin order list all need it, and none of them should
	// have to join payments to find out.
	PaymentMethod string
	// When the cash actually arrived, on a cash-on-delivery order. Nil while
	// it is still owed, and on every card order.
	CashCollectedAt *time.Time
	CashCollectedBy *string

	IdempotencyKey string
	MarketingOptIn bool
	Notes          *string

	SettledBy     *string
	SettledByName *string
	// When the order was fully paid: the last till payment, the cash
	// collected at the door, or the card capture. Nil while money is owed.
	SettledAt *time.Time
	// The cancellation, read from the status timeline so it is there for
	// every cancel path (till, admin, customer, system) and for orders
	// cancelled before these fields existed. Nil unless the order is
	// cancelled.
	Cancellation *OrderCancellation
	// BusinessID is the business the order belongs to (its store's), set by
	// the database when the order is created.
	BusinessID        *string
	ReturnedBy        *string
	ReturnedByName    *string
	TotalDurationSecs *int
	// StartTime is when the order was opened; EndTime when it first reached
	// a closing state (settled, fulfilled, cancelled, returned). Nil until
	// then. Stored columns, not derived from created_at/updated_at.
	StartTime *time.Time
	EndTime   *time.Time

	// Who rang the order up at the till (staff id from the token at create)
	// and who last touched it from the till (staff id from the token on
	// every POS update, split, return or payment).
	BillerID          *string
	BillerName        *string
	UpdatedBillerID   *string
	UpdatedBillerName *string

	// How the bill was split, if it was: amount, item or custom. Nil for an
	// order settled in one go.
	SplitMode *string

	GuestCustomerName    *string
	GuestCustomerMobile  *string
	GuestCustomerAddress *string
	GuestCustomerEmail   *string
	// The city and postcode typed at the till for this order.
	GuestCustomerCity     *string
	GuestCustomerPostcode *string
	// And the street and state.
	GuestCustomerStreet *string
	GuestCustomerState  *string
	OrderDeliverDate    *string
	OrderDeliverTime    *string

	CreatedAt time.Time
	UpdatedAt time.Time

	// KDS: the kitchen's own status for the order (see kds.go) and when it
	// moved through the kitchen. "queue" until a KDS starts it.
	KOTStatus      string
	KOTStartedAt   *time.Time
	KOTReadyAt     *time.Time
	KOTCompletedAt *time.Time
	// KDS: changed by POS/admin while the kitchen had it. The KDS shows it as
	// "Amended" until it is acknowledged.
	IsAmended             bool
	AmendedAt             *time.Time
	AmendedAcknowledged   bool
	AmendedAcknowledgedAt *time.Time

	// Order-based return totals: the sum over every return on the order
	// (gross, discount, tax, refund). The percentages are the rates applied
	// on the most recent return. Zero / nil with no returns.
	ReturnAmountCents   int64
	ReturnDiscountPct   *float64
	ReturnDiscountCents int64
	ReturnTaxPct        *float64
	ReturnTaxCents      int64
	ReturnTotalCents    int64

	// Return-based: each return with its own tax and discount. Populated by
	// GetOrderByID; left empty by the list query.
	Returns []OrderReturn

	// ItemCount is populated by ListOrders (a cheap subquery) for list
	// views that intentionally don't load the full Items slice; GetOrderByID
	// and GetOrderByReference populate Items instead and leave this at 0 —
	// callers needing a count from a full order should use len(Items).
	ItemCount int

	Items           []OrderItem
	Timeline        []OrderStatusEvent
	BillIDs         []string
	PendingBillIDs  []PendingBillRef
	TotalBillsCount int
	BillNo          *int

	// The full split and every tender taken, populated by GetOrderByID so a
	// single order read can show the bills (with their item lines for an
	// item split) and the payments without a second call. Left empty by the
	// list query.
	Bills    []Bill
	Payments []BillPayment
}

type PendingBillRef struct {
	ID          string `json:"id"`
	AmountCents int64  `json:"amountCents"`
}

type BillSummary struct {
	ID          string
	AmountCents int64
	PaidCents   int64
	Status      string
}

type OrderItem struct {
	ID                   string
	OrderID              string
	ProductID            string
	ProductName          string
	VariantID            *string
	VariantName          *string
	Quantity             int
	UnitPriceCents       int64
	LineTotalCents       int64
	ItemDiscountCents    int64
	ItemDiscountPct      *float64
	SelectedFlavours     []SelectedModifier
	SelectedAddons       []SelectedModifier
	SelectedPresentation *SelectedModifier
	SelectedWrapColour   *SelectedModifier
	ProductionStatus     string
	// Which trip to the kitchen this line was on. 1 for everything ordered
	// together; a second round against the same order is batch 2.
	BatchNo int
	// How many of this line have been handed back over the counter.
	ReturnedQuantity int
	// When the till sent this line to the kitchen. Recorded as the till
	// reports it rather than stamped here: the POS screen decides when a
	// line goes, and it may be sending a batch it printed a minute ago.
	SendKotTime *time.Time
	// When the till cancelled this line, as the till reported it. Nil for a
	// line that has not been cancelled.
	CancelledAt         *time.Time
	ProductionStartedAt *time.Time
	ProductionDoneAt    *time.Time
	Comments            *string

	// KDS: the line's kitchen status and when it moved.
	// Gift message and occasion for this line (storefront per-line gift, POS).
	GiftMessage  *string
	GiftReason   *string
	KOTStatus    string
	KOTStartedAt *time.Time
	KOTReadyAt   *time.Time
	// KDS: the product's preparation window and category, read live from the
	// catalogue (nil product id → zeros / empty).
	MinPrepTime     int
	MinPrepTimeUnit string
	MaxPrepTime     int
	MaxPrepTimeUnit string
	MinPrepMinutes  int
	MaxPrepMinutes  int
	CategoryID      string
	CategoryName    string
}

type OrderStatusEvent struct {
	ID        string
	OrderID   string
	Status    string
	ActorID   *string
	ActorType string
	Note      *string
	CreatedAt time.Time
}

type OrderAmendment struct {
	ID              string
	OrderID         string
	ActorID         *string
	ActorRole       string
	Reason          string
	BeforeValue     map[string]interface{}
	AfterValue      map[string]interface{}
	PriceDeltaCents int64
	CreatedAt       time.Time
}

// PriceSummary is the canonical order-total breakdown shared by cart and
// checkout responses.
type PriceSummary struct {
	SubtotalCents    int64
	DiscountCents    int64
	DiscountPct      *float64
	DeliveryFeeCents int64
	TaxCents         int64
	TaxPct           *float64
	// What this outlet calls its tax. A basket that says "Tax" where the
	// receipt will say "NJ Sales Tax" is a small thing to get wrong twice.
	TaxName            string
	ServiceChargeCents int64
	TipCents           int64
	TotalCents         int64

	// For a delivery cart: whether the store delivers to its address, and if
	// not, why (a customer-readable sentence). Always available for pickup.
	DeliveryAvailable bool
	DeliveryMessage   string

	// Currency the amounts are in: the store's business's.
	Currency string
}

// OrderCancellation is who cancelled an order, when, and why.
type OrderCancellation struct {
	At time.Time `json:"at"`
	// Staff or customer id; nil for the system.
	ByID *string `json:"actorId"`
	// staff, customer or system.
	ByType string `json:"actorType"`
	// The staff member's or customer's name (email when they have none).
	ByName *string `json:"name"`
	Reason *string `json:"note"`
}
