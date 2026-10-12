package ordersapi

import (
	"strings"
	"time"
)

// The shapes the till sends and receives.
//
// Money is in minor units throughout, like everywhere else in this codebase.
// Percentages are the one exception: a discount is chosen as "10%", the
// receipt has to say "10%", and rounding it into cents on the way in would
// make it impossible to say afterwards what was actually offered.

// POSItem is one line as the till rings it up.
//
// The prices come from the till rather than being looked up, because the
// operator is standing in front of the customer with a figure on the screen
// and that figure is what was agreed. The catalogue is where those numbers
// came from a moment ago; re-deriving them here would only introduce the
// possibility of the screen and the receipt disagreeing.
type POSItem struct {
	// Gift message and occasion for this line. On an update nil keeps and ""
	// clears.
	GiftMessage    *string
	GiftReason     *string
	ProductID      string
	ProductName    string
	VariantID      string
	VariantName    string
	Quantity       int
	UnitPriceCents int64
	LineTotalCents int64

	// Chosen options, stored as they are on any other order line so the
	// kitchen ticket and the admin both read them the same way.
	Flavours []SelectedModifierRef
	Addons   []SelectedModifierRef

	// Per-item discount. Both are kept: the percentage is what was chosen and
	// what a receipt should print, the amount is what came off and must not
	// drift if the order is edited later.
	DiscountPct   *float64
	DiscountCents int64

	/*
	 * What the till decides about the line, stored as it says.
	 *
	 * The POS screen holds an order while it is being worked on: it knows
	 * which round a line is on, when it went to the kitchen and whether it
	 * has since been cancelled. Deriving any of that here would mean the
	 * server second-guessing a screen that already has the answer.
	 *
	 * ID is empty for a new line and set for one being edited — which is
	 * what tells an update whether to insert or to change.
	 */
	ID string
	// Zero on create, where the order's first batch is used.
	BatchNo     int
	SendKotTime *time.Time
	// pending, in_progress, done or cancelled. Empty means pending.
	Status   string
	Comments string
	// When the till cancelled the line. Stored as sent.
	CancelTime          *time.Time
	ProductionStartedAt *time.Time
	ProductionDoneAt    *time.Time
	// KDS: the line's kitchen status as the frontend sends it. Empty on a new
	// line means "queue"; empty on an existing line leaves it as it is.
	KOTStatus string
}

// The states a line can be in. The till sets these; the server stores them.
// A cancelled line stays on the order — the kitchen was handed a docket with
// it on — but stops counting towards the total.
const (
	ItemPending    = "pending"
	ItemInProgress = "in_progress"
	ItemDone       = "done"
	ItemCancelled  = "cancelled"
	ItemCancel     = "cancel"
	ItemReturned   = "returned"
)

// NormalizeItemStatus normalizes item status strings.
func NormalizeItemStatus(s string) string {
	lower := strings.ToLower(strings.TrimSpace(s))
	switch lower {
	case "cancel":
		return ItemCancel
	case "cancelled":
		return ItemCancelled
	case "return", "returned":
		return ItemReturned
	case "in_progress", "inprogress", "in-progress":
		return ItemInProgress
	case "done", "completed":
		return ItemDone
	case "pending":
		return ItemPending
	default:
		return s
	}
}

// IsValidItemStatus reports whether a string is one of them.
func IsValidItemStatus(s string) bool {
	switch NormalizeItemStatus(s) {
	case ItemPending, ItemInProgress, ItemDone, ItemCancelled, ItemCancel, ItemReturned:
		return true
	}
	return false
}

// SelectedModifierRef is a chosen option, denormalised onto the line.
type SelectedModifierRef struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	PriceDelta int64  `json:"priceDelta"`
	GroupName  string `json:"groupName,omitempty"`
}

// OrderTotals is the arithmetic, computed on the server from the request.
type OrderTotals struct {
	SubtotalCents int64
	DiscountCents int64
	DiscountPct   *float64
	TaxCents      int64
	// TaxPct is the rate tax is computed at. When set and TaxExplicit is
	// false, TaxCents is recomputed from it on every total recalculation,
	// so a line discount added later moves the tax with it.
	TaxPct *float64
	// TaxExplicit is true when the till sent taxCents itself for this
	// change; the cents are then taken as-is. Not persisted.
	TaxExplicit        bool
	DeliveryFeeCents   int64
	ServiceChargeCents int64
	TipCents           int64
	TotalCents         int64
}

// OrderLineAmount is what a stored line contributes, for recomputing a total.
type OrderLineAmount struct {
	ID               string
	Quantity         int
	ReturnedQuantity int
	UnitPriceCents   int64
	LineTotalCents   int64
	DiscountCents    int64
	DiscountPct      *float64
}

// ReturnLine is one item being handed back.
type ReturnLine struct {
	OrderItemID string `json:"orderItemId"`
	Quantity    int    `json:"quantity"`
}

// ReturnRequest is a whole order coming back, or some of its lines.
type ReturnRequest struct {
	// When true the lines are resolved server-side to everything still
	// outstanding, so the till does not have to enumerate them.
	FullReturn bool
	Reason     string
	Items      []ReturnLine

	// Discount and tax on the goods coming back, as the till states them.
	// All optional: a cents value wins over its percentage; with neither,
	// the discount follows the order's discount (rate, or a proportional
	// share of a fixed amount) and the tax follows the order's tax rate.
	DiscountPct   *float64
	DiscountCents *int64
	TaxPct        *float64
	TaxCents      *int64

	// Refund says how the money for this return goes back. Omitted: the
	// return moves no money, exactly as before.
	Refund *ReturnRefund
}

// ReturnRefund: how a return is refunded.
//
//	PaymentMode  cash | card
//	CardType     stripe | manual (card only; manual = own terminal)
//	RefundType   full (the return's refund) | partial (AmountCents of it)
//
// Only stripe moves money through a provider, and the return is
// recorded only once the provider has accepted the refund.
type ReturnRefund struct {
	PaymentMode string
	CardType    string
	RefundType  string
	AmountCents int64
}

// Refund modes.
const (
	RefundModeCash = "cash"
	RefundModeCard = "card"
	// Back the way the order was paid: its Stripe payments first, as
	// far as they go, the rest in cash at the counter. One mode for a single
	// tender and for a split paid with several.
	RefundModeOriginal = "original"
	RefundTypeFull     = "full"
	RefundTypePart     = "partial"
	CardTypeManual     = "manual"
	CardTypeStripe     = "stripe"
)

// ReturnCharges is the money on one return: the gross value of the goods
// (unit price × quantity), the discount and tax on them, and the refund.
//
//	RefundCents = GrossCents - DiscountCents + TaxCents
type ReturnCharges struct {
	GrossCents    int64
	DiscountPct   *float64
	DiscountCents int64
	TaxPct        *float64
	TaxCents      int64
	RefundCents   int64
}

// ReturnLineInput is what is known about one line being handed back, read
// from the order line inside the return's transaction.
type ReturnLineInput struct {
	OrderItemID string
	// Quantity coming back now.
	Quantity       int
	UnitPriceCents int64
	// The whole line as sold: its quantity and its own discount (cents for
	// the whole line, and the rate when one was chosen).
	LineQuantity      int
	LineDiscountCents int64
	LineDiscountPct   *float64
}

// ReturnLineCharges is one returned line priced on its own: its gross
// (unit price × returned quantity), its discount (the line's own, prorated by
// the quantity returned, plus its share of the order discount), the tax on
// what is left, and its refund.
type ReturnLineCharges struct {
	OrderItemID string
	Quantity    int
	ReturnCharges
}

// ReturnPricer prices the lines of a return, each on its own, and gives the
// return's totals (the sums of the lines). The store calls it inside the
// return's transaction, once the lines are locked and read.
type ReturnPricer func(lines []ReturnLineInput) ([]ReturnLineCharges, ReturnCharges)

// OrderReturn is one return as recorded, for the order read.
type OrderReturn struct {
	ID             string
	Reason         *string
	IsFullReturn   bool
	ReturnedBy     *string
	ReturnedByName *string
	CreatedAt      time.Time
	ReturnCharges
	Items []OrderReturnItem
}

// OrderReturnItem is one line on a return, with its own discount, tax and
// refund. AmountCents is the gross (unit price × quantity returned).
type OrderReturnItem struct {
	OrderItemID   string
	ProductName   string
	Quantity      int
	AmountCents   int64
	DiscountPct   *float64
	DiscountCents int64
	TaxPct        *float64
	TaxCents      int64
	RefundCents   int64
}

// SplitMode is how a bill was arrived at.
//
// Kept only so a receipt can explain itself — by the time it is a bill it is
// an amount somebody owes, however it was reached.
const (
	SplitByAmount = "amount"
	SplitByItem   = "item"
	SplitCustom   = "custom"
)

// Bill is one share of an order, settled on its own.
type Bill struct {
	ID          string
	OrderID     string
	SplitMode   string
	Label       string
	SequenceNo  int
	AmountCents int64
	PaidCents   int64
	Status      string
	Items       []BillItem
	Payments    []BillPayment
}

// BillItem is a line assigned to an item-split bill.
type BillItem struct {
	OrderItemID string
	ProductName string
	Quantity    int
	AmountCents int64
}

// BillPayment is money taken at the counter.
type BillPayment struct {
	ID            string
	BillID        *string
	Method        string
	AmountCents   int64
	TipCents      int64
	TenderedCents *int64
	// What to hand back, for a cash payment where more was offered than owed.
	ChangeCents int64
	Reference   string
	TakenBy     *string
	CreatedAt   string
	// Card only: who took it — "stripe" (a payment link), or
	// "manual" (the store's own terminal; nothing to refund through an API).
	Provider      *string
	ProviderRef   *string // the provider's payment id
	PaymentLinkID *string
}

// POSOrderShell is the non-line part of a counter order.
//
// Thin on purpose: a walk-in has no address, no slot and often no name. The
// fields that do arrive are denormalised onto the order like every other
// channel, so admin, KDS and the website all read it without knowing where it
// came from.
type POSOrderShell struct {
	FulfilmentType        string
	FirstName             string
	LastName              string
	Phone                 string
	Email                 string
	Address               string
	GuestCustomerName     *string
	GuestCustomerMobile   *string
	GuestCustomerAddress  *string
	GuestCustomerCity     *string
	GuestCustomerPostcode *string
	GuestCustomerEmail    *string
	// The customer's street and state as typed: kept on the order
	// (guest_customer_street/state) and on the customer's address record.
	CustomerStreet   *string
	CustomerState    *string
	OrderDeliverDate *string
	OrderDeliverTime *string
	ScheduledDate    *time.Time
	SlotStart        *string
	SlotEnd          *string
	Notes            string
	// Staff id from the token of whoever rang the order up.
	BillerID *string
	Status   string
	// Who the order is for, when the till names somebody other than the
	// customer (a gift). Nil keeps the old behaviour: the recipient is the
	// customer.
	Recipient *POSRecipient
	// Gift details, as on the storefront. Nil leaves the column's default.
	GiftMessage *string
	GiftReason  *string
	IsGift      *bool
}

// POSRecipient is the person an order is for — the same fields the
// storefront checkout takes. On an update a nil field keeps what the order
// has.
type POSRecipient struct {
	FirstName *string
	LastName  *string
	Phone     *string
	Email     *string
	// Where the order goes, stored as the order's delivery address
	// (delivery_street / delivery_city / delivery_postcode), the same columns
	// the storefront uses.
	Address  *string
	City     *string
	Postcode *string
}

/*
 * OrderTotalsPatch is a partial set of charges.
 *
 * A nil field means "leave what the order already has". This exists because
 * adding a second kitchen batch used to take a whole OrderTotals: a till that
 * sent only the new lines — which is the obvious thing for "add these items"
 * to send — had its tax, service charge and tip silently overwritten with the
 * zero value, and the counter then undercharged by exactly those amounts.
 *
 * Omitting a charge and setting it to zero are different requests, so they
 * need to be different values on the wire.
 */
type OrderTotalsPatch struct {
	DiscountPct        *float64
	DiscountCents      *int64
	TaxCents           *int64
	TaxPct             *float64
	DeliveryFeeCents   *int64
	ServiceChargeCents *int64
	TipCents           *int64
}

// Apply overlays whatever the patch actually carries onto the current charges.
func (p OrderTotalsPatch) Apply(cur OrderTotals) OrderTotals {
	if p.DiscountPct != nil {
		cur.DiscountPct = p.DiscountPct
	}
	if p.DiscountCents != nil {
		cur.DiscountCents = *p.DiscountCents
	}
	if p.TaxPct != nil {
		cur.TaxPct = p.TaxPct
	}
	if p.TaxCents != nil {
		cur.TaxCents = *p.TaxCents
		cur.TaxExplicit = true
	}
	if p.DeliveryFeeCents != nil {
		cur.DeliveryFeeCents = *p.DeliveryFeeCents
	}
	if p.ServiceChargeCents != nil {
		cur.ServiceChargeCents = *p.ServiceChargeCents
	}
	if p.TipCents != nil {
		cur.TipCents = *p.TipCents
	}
	return cur
}
