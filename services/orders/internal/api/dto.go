package api

import (
	"math"
	"strings"
	"time"

	"bakehouse/services/orders/ordersapi"
)

const dateFmt = "2006-01-02"
const rfc3339 = "2006-01-02T15:04:05Z07:00"

type modifierDTOType struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	PriceDelta int64  `json:"priceDelta"`
}

func modDTO(m ordersapi.SelectedModifier) modifierDTOType {
	return modifierDTOType{ID: m.ID, Name: m.Name, PriceDelta: m.PriceDelta}
}
func modDTOList(m []ordersapi.SelectedModifier) []modifierDTOType {
	out := make([]modifierDTOType, 0, len(m))
	for _, x := range m {
		out = append(out, modDTO(x))
	}
	return out
}
func modDTOPtr(m *ordersapi.SelectedModifier) *modifierDTOType {
	if m == nil {
		return nil
	}
	d := modDTO(*m)
	return &d
}

type priceSummaryDTOType struct {
	SubtotalCents      int64   `json:"subtotal"`
	DiscountCents      int64   `json:"discount"`
	DiscountPct        float64 `json:"discountPct"`
	DeliveryFeeCents   int64   `json:"deliveryFee"`
	TaxCents           int64   `json:"tax"`
	TaxPct             float64 `json:"taxPct"`
	TaxName            string  `json:"taxName"`
	ServiceChargeCents int64   `json:"serviceCharge"`
	TipCents           int64   `json:"tip"`
	TotalCents         int64   `json:"total"`
	Currency           string  `json:"currency"`
	// False when the store doesn't deliver to the cart's address (or none is
	// set yet); deliveryMessage then says why. Always true for pickup.
	DeliveryAvailable bool   `json:"deliveryAvailable"`
	DeliveryMessage   string `json:"deliveryMessage,omitempty"`
}

// defaultCurrency is shown for amounts with no currency recorded: the
// product's launch market.
const defaultCurrency = "GBP"

func currencyOr(c string) string {
	if c == "" {
		return defaultCurrency
	}
	return c
}

func priceSummaryDTO(p *ordersapi.PriceSummary) priceSummaryDTOType {
	if p == nil {
		return priceSummaryDTOType{Currency: defaultCurrency}
	}
	var discountPct float64
	if p.DiscountPct != nil {
		discountPct = *p.DiscountPct
	} else if p.SubtotalCents > 0 && p.DiscountCents > 0 {
		discountPct = math.Round(float64(p.DiscountCents)*10000/float64(p.SubtotalCents)) / 100
	}

	var taxPct float64
	if p.TaxPct != nil {
		taxPct = *p.TaxPct
	} else {
		taxable := p.SubtotalCents - p.DiscountCents
		if taxable > 0 && p.TaxCents > 0 {
			taxPct = math.Round(float64(p.TaxCents)*10000/float64(taxable)) / 100
		}
	}

	return priceSummaryDTOType{
		SubtotalCents: p.SubtotalCents, DiscountCents: p.DiscountCents, DiscountPct: discountPct,
		DeliveryFeeCents: p.DeliveryFeeCents, TaxCents: p.TaxCents, TaxPct: taxPct,
		TaxName: taxLabel(p.TaxName), ServiceChargeCents: p.ServiceChargeCents, TipCents: p.TipCents, TotalCents: p.TotalCents, Currency: currencyOr(p.Currency),
		DeliveryAvailable: p.DeliveryAvailable, DeliveryMessage: p.DeliveryMessage,
	}
}

// --- Cart --------------------------------------------------------------------

type cartItemDTOType struct {
	ID          string `json:"cartItemId"`
	ProductID   string `json:"productId"`
	ProductName string `json:"productName"`
	// ProductSlug is what the cart screen's "Edit" link routes to —
	// /products/{slug}. Null only if the product has since been deleted.
	ProductSlug  *string           `json:"productSlug"`
	ProductImage *string           `json:"productImage"`
	VariantID    *string           `json:"variantId"`
	VariantName  *string           `json:"variantName"`
	Flavours     []modifierDTOType `json:"flavours"`
	Addons       []modifierDTOType `json:"addons"`
	Presentation *modifierDTOType  `json:"presentation"`
	WrapColour   *modifierDTOType  `json:"wrapColour"`
	Quantity     int               `json:"quantity"`
	UnitPrice    int64             `json:"unitPrice"`
	LineTotal    int64             `json:"lineTotal"`
	IsGift       bool              `json:"isGift"`
	Available    bool              `json:"available"`
	// Set when a seasonal tag's restricted dates rule this line out for the
	// cart's chosen slot: "Unavailable Nov 25", available again from a date.
	Restriction *itemRestrictionDTOType `json:"restriction"`
	Recipient   *recipientDTOType       `json:"recipient,omitempty"`
	GiftMessage *string                 `json:"giftMessage"`
}

type recipientDTOType struct {
	FirstName *string `json:"firstName"`
	LastName  *string `json:"lastName"`
}

func cartItemDTO(i *ordersapi.CartItem) cartItemDTOType {
	out := cartItemDTOType{
		ID: i.ID, ProductID: i.ProductID, ProductName: i.ProductName,
		ProductSlug: i.ProductSlug, ProductImage: i.ProductImageURL,
		VariantID: i.VariantID, VariantName: i.VariantName, Flavours: modDTOList(i.SelectedFlavours), Addons: modDTOList(i.SelectedAddons),
		Presentation: modDTOPtr(i.SelectedPresentation), WrapColour: modDTOPtr(i.SelectedWrapColour),
		Quantity: i.Quantity, UnitPrice: i.UnitPriceCents, LineTotal: i.LineTotalCents(),
		IsGift: i.IsGift, Available: i.Available, GiftMessage: i.GiftMessage,
	}
	if i.RecipientFirstName != nil || i.RecipientLastName != nil {
		out.Recipient = &recipientDTOType{FirstName: i.RecipientFirstName, LastName: i.RecipientLastName}
	}
	if r := i.Restriction; r != nil {
		out.Restriction = &itemRestrictionDTOType{TagID: r.TagID, TagName: r.TagName, WindowFrom: r.WindowFrom,
			WindowTo: r.WindowTo, AvailableFrom: r.AvailableFrom, Message: r.Message}
	}
	return out
}

type itemRestrictionDTOType struct {
	TagID         string `json:"tagId"`
	TagName       string `json:"tagName"`
	WindowFrom    string `json:"windowFrom" example:"2026-11-24T00:30:00"`
	WindowTo      string `json:"windowTo" example:"2026-11-27T13:00:00"`
	AvailableFrom string `json:"availableFrom" example:"2026-11-28"`
	Message       string `json:"message"`
}

type cartDTOType struct {
	CartID          string              `json:"cartId"`
	BranchID        string              `json:"branchId"`
	FulfilmentType  string              `json:"fulfilmentType"`
	DeliveryAddress *addressDTOType     `json:"deliveryAddress,omitempty"`
	ScheduledDate   *string             `json:"scheduledDate"`
	SlotStart       *string             `json:"slotStart"`
	SlotEnd         *string             `json:"slotEnd"`
	Items           []cartItemDTOType   `json:"items"`
	Summary         priceSummaryDTOType `json:"summary"`
	// The applied discount code, so the basket can show it as applied with a
	// way to remove it rather than leaving an empty box over a discounted
	// total. Null when none is applied.
	DiscountCode *string `json:"discountCode"`
}

type addressDTOType struct {
	Street   *string `json:"street"`
	City     *string `json:"city"`
	State    *string `json:"state"`
	Postcode *string `json:"postcode"`
}

func cartDTO(c *ordersapi.Cart, summary *ordersapi.PriceSummary) cartDTOType {
	out := cartDTOType{
		CartID: c.ID, BranchID: c.BranchID, FulfilmentType: c.FulfilmentType,
		Summary: priceSummaryDTO(summary), DiscountCode: c.DiscountCode,
	}
	if c.DeliveryStreet != nil {
		out.DeliveryAddress = &addressDTOType{Street: c.DeliveryStreet, City: c.DeliveryCity, State: c.DeliveryState, Postcode: c.DeliveryPostcode}
	}
	if c.ScheduledDate != nil {
		d := c.ScheduledDate.Format(dateFmt)
		out.ScheduledDate = &d
	}
	out.SlotStart = c.SlotStart
	out.SlotEnd = c.SlotEnd
	for _, i := range c.Items {
		out.Items = append(out.Items, cartItemDTO(&i))
	}
	if out.Items == nil {
		out.Items = []cartItemDTOType{}
	}
	return out
}

// --- Slots ---------------------------------------------------------------------

type slotDTOType struct {
	Start     string `json:"start"`
	End       string `json:"end"`
	Remaining int    `json:"remaining"`
}

type daySlotsDTOType struct {
	Date  string        `json:"date"`
	Label string        `json:"label"`
	Slots []slotDTOType `json:"slots"`
}

func daySlotsDTO(d *ordersapi.DaySlots) daySlotsDTOType {
	out := daySlotsDTOType{Date: d.Date, Label: d.Label}
	for _, s := range d.Slots {
		out.Slots = append(out.Slots, slotDTOType{Start: s.SlotStart, End: s.SlotEnd, Remaining: s.Remaining()})
	}
	return out
}

type scheduleConfigDTOType struct {
	SlotDurationMins   int    `json:"slotDurationMins"`
	PickupCutoffTime   string `json:"pickupCutoffTime"`
	DeliveryCutoffTime string `json:"deliveryCutoffTime"`
	MaxOrdersPerSlot   int    `json:"maxOrdersPerSlot"`
	DefaultLeadHours   int    `json:"defaultLeadHours"`
	AdvanceDaysMax     int    `json:"advanceDaysMax"`
}

func scheduleConfigDTO(c *ordersapi.ScheduleConfig) scheduleConfigDTOType {
	return scheduleConfigDTOType{
		SlotDurationMins: c.SlotDurationMins, PickupCutoffTime: c.PickupCutoffTime, DeliveryCutoffTime: c.DeliveryCutoffTime,
		MaxOrdersPerSlot: c.MaxOrdersPerSlot, DefaultLeadHours: c.DefaultLeadHours, AdvanceDaysMax: c.AdvanceDaysMax,
	}
}

// --- Orders --------------------------------------------------------------------

type orderItemDTOType struct {
	// The line's own id. Needed by anything that acts on one line rather than
	// the whole order — returning an item, or putting it on a split bill —
	// which had no way to name what it meant without this.
	ID        string  `json:"id"`
	ProductID string  `json:"productId"`
	VariantID *string `json:"variantId"`
	// Which trip to the kitchen this line was on, so a ticket can show what is
	// new and the screen can group by round.
	BatchNo          int `json:"batchNo"`
	ReturnedQuantity int `json:"returnedQuantity"`
	// When this line was sent to the kitchen, as the till recorded it. Null
	// for a line that has not been sent.
	SendKotTime *string `json:"sendKotTime"`
	// When the till cancelled this line. Null for a line still standing.
	CancelTime          *string           `json:"cancelTime"`
	ProductionStartedAt *string           `json:"productionStartedAt"`
	ProductionDoneAt    *string           `json:"productionDoneAt"`
	ProductName         string            `json:"productName"`
	VariantName         *string           `json:"variantName"`
	Flavours            []modifierDTOType `json:"flavours"`
	Addons              []modifierDTOType `json:"addons"`
	Presentation        *modifierDTOType  `json:"presentation"`
	WrapColour          *modifierDTOType  `json:"wrapColour"`
	Quantity            int               `json:"quantity"`
	UnitPrice           int64             `json:"unitPriceCents"`
	LineTotal           int64             `json:"lineTotalCents"`
	ItemDiscountCents   int64             `json:"itemDiscountCents"`
	ItemDiscountPct     *float64          `json:"itemDiscountPct"`
	Comments            *string           `json:"comments"`
	Status              string            `json:"status"`
	ProductionStatus    string            `json:"productionStatus"`
	// KDS: the line's kitchen status (queue, preparing, ready, completed,
	// cancelled) and when it moved.
	KotStatus    string  `json:"kotStatus"`
	KotStartedAt *string `json:"kotStartedAt"`
	KotReadyAt   *string `json:"kotReadyAt"`
	// This line's gift message and occasion. Null when none.
	GiftMessage *string `json:"giftMessage"`
	GiftReason  *string `json:"giftReason"`
}

func orderItemDTO(i *ordersapi.OrderItem) orderItemDTOType {
	status := i.ProductionStatus

	if status == "" {
		status = ordersapi.ItemPending
	}
	return orderItemDTOType{
		ID: i.ID, ProductID: i.ProductID, VariantID: i.VariantID, BatchNo: i.BatchNo,
		ReturnedQuantity: i.ReturnedQuantity, SendKotTime: fmtTimePtr(i.SendKotTime), CancelTime: fmtTimePtr(i.CancelledAt),
		ProductionStartedAt: fmtTimePtr(i.ProductionStartedAt), ProductionDoneAt: fmtTimePtr(i.ProductionDoneAt),
		ProductName: i.ProductName, VariantName: i.VariantName, Flavours: modDTOList(i.SelectedFlavours), Addons: modDTOList(i.SelectedAddons),
		Presentation: modDTOPtr(i.SelectedPresentation), WrapColour: modDTOPtr(i.SelectedWrapColour),
		Quantity: i.Quantity, UnitPrice: i.UnitPriceCents, LineTotal: i.LineTotalCents,
		ItemDiscountCents: i.ItemDiscountCents, ItemDiscountPct: i.ItemDiscountPct,
		Status:           status,
		ProductionStatus: status,
		Comments:         i.Comments,
		KotStatus:        orDefaultStr(i.KOTStatus, ordersapi.ItemKOTQueue),
		KotStartedAt:     fmtTimePtr(i.KOTStartedAt),
		KotReadyAt:       fmtTimePtr(i.KOTReadyAt),
		GiftMessage:      i.GiftMessage,
		GiftReason:       i.GiftReason,
	}
}

func orDefaultStr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

type timelineEventDTOType struct {
	Status string `json:"status"`
	At     string `json:"at"`
	Actor  string `json:"actor"`
	Note   string `json:"note,omitempty"`
}

type orderDTOType struct {
	ID        string `json:"id"`
	Reference string `json:"reference"`
	BillNo    *int   `json:"billNo" example:"42"`
	// OrderNo is the per-outlet running number — the same value as billNo,
	// under the name the till calls it.
	OrderNo       *int   `json:"orderNo" example:"42"`
	BranchID      string `json:"branchId"`
	Status        string `json:"status"`
	PaymentStatus string `json:"paymentStatus"`
	// PaymentMethod distinguishes a cash-on-delivery order from a card one
	// without a join. "pending" against "cash_on_delivery" is money still
	// owed; against "card" it means a capture that never completed.
	PaymentMethod   string  `json:"paymentMethod"`
	PaymentType     string  `json:"paymentType"`
	CashCollectedAt *string `json:"cashCollectedAt"`
	FulfilmentType  string  `json:"fulfilmentType"`
	Channel         string  `json:"channel"`
	CustomerID      *string `json:"customerId"`
	Currency        string  `json:"currency"`
	Notes           *string `json:"notes"`

	Recipient   recipientNameDTOType `json:"recipient"`
	GiftMessage *string              `json:"giftMessage"`
	GiftReason  *string              `json:"giftReason"`
	IsGift      bool                 `json:"isGift"`
	Purchaser   purchaserDTOType     `json:"purchaser"`
	Customer    customerOrderDTOType `json:"customer"`

	DeliveryAddress      *addressDTOType `json:"deliveryAddress"`
	DeliveryInstructions *string         `json:"deliveryInstructions"`
	BusinessName         *string         `json:"businessName"`
	DeliveryAddressType  *string         `json:"deliveryAddressType"`
	PhotoOnDelivery      bool            `json:"photoOnDelivery"`
	SurpriseDelivery     bool            `json:"surpriseDelivery"`

	ScheduledDate string `json:"scheduledDate"`
	SlotStart     string `json:"slotStart"`
	SlotEnd       string `json:"slotEnd"`

	Summary priceSummaryDTOType `json:"summary"`
	// TotalCents mirrors summary.total at the top level so callers that
	// only need the bottom-line number do not have to unwrap summary.
	TotalCents int64 `json:"totalCents"`
	// Service charge and tip at the top level for the same reason.
	ServiceChargeCents int64 `json:"serviceChargeCents"`
	TipCents           int64 `json:"tipCents"`
	// What has been handed back: units and what they were sold for
	// (returned quantity × unit price, over every line). Zero on an order
	// with no returns.
	ReturnedQuantity   int   `json:"returnedQuantity"`
	ReturnedTotalCents int64 `json:"returnedTotalCents"`
	// Order based: the discount, tax and refund over every return on the
	// order. Return based: each return with its own discount and tax.
	ReturnSummary   returnChargesDTOType `json:"returnSummary"`
	Returns         []orderReturnDTOType `json:"returns"`
	DiscountCode    *string              `json:"discountCode"`
	Items           []orderItemDTOType   `json:"items"`
	BillIDs         []string             `json:"billIds"`
	PendingBillIDs  []pendingBillDTOType `json:"pendingBillIds"`
	TotalBillsCount int                  `json:"totalBillsCount"`
	// How the bill was split (amount, item, custom), or null if it was not.
	SplitMode *string `json:"splitMode"`
	// The split itself — each bill with its lines for an item split — and
	// every payment taken against the order.
	Bills    []billDTOType        `json:"bills"`
	Payments []billPaymentDTOType `json:"payments"`

	Timeline []timelineEventDTOType `json:"statusTimeline"`

	CanAmend  bool `json:"canAmend"`
	CanCancel bool `json:"canCancel"`

	CreatedAt      string  `json:"createdAt"`
	StartTime      string  `json:"startTime"`
	EndTime        string  `json:"endTime"`
	TotalDuration  *int    `json:"totalDuration"`
	SettledBy      *string `json:"settledBy"`
	SettledByName  *string `json:"settledByName"`
	SettledByID    *string `json:"settledById"`
	ReturnedBy     *string `json:"returnedBy"`
	ReturnedByName *string `json:"returnedByName"`
	ReturnedByID   *string `json:"returnedById"`
	// When it was fully paid, and who cancelled it, when and why.
	settleCancelDTO

	// Who rang the order up and who last touched it from the till.
	BillerID          *string `json:"billerId"`
	BillerName        *string `json:"billerName"`
	UpdatedBillerID   *string `json:"updatedBillerId"`
	UpdatedBillerName *string `json:"updatedBillerName"`

	GuestCustomerName     *string `json:"guestCustomerName" example:"Jane Doe"`
	GuestCustomerMobile   *string `json:"guestCustomerMobile" example:"+1234567890"`
	GuestCustomerAddress  *string `json:"guestCustomerAddress" example:"123 Flower Street"`
	GuestCustomerEmail    *string `json:"guestCustomerEmail" example:"guest@example.com"`
	GuestCustomerCity     *string `json:"guestCustomerCity" example:"Edison"`
	GuestCustomerPostcode *string `json:"guestCustomerPostcode" example:"08817"`
	OrderDeliverDate      *string `json:"orderDeliverDate" example:"2026-09-07"`
	OrderDeliverTime      *string `json:"orderDeliverTime" example:"14:30:00"`

	// KDS: the kitchen status and the amended flag.
	KotStatus             string  `json:"kotStatus" example:"queue"`
	KotStartedAt          *string `json:"kotStartedAt"`
	KotReadyAt            *string `json:"kotReadyAt"`
	KotCompletedAt        *string `json:"kotCompletedAt"`
	IsAmended             bool    `json:"isAmended"`
	AmendedAt             *string `json:"amendedAt"`
	AmendedAcknowledged   bool    `json:"amendedAcknowledged"`
	AmendedAcknowledgedAt *string `json:"amendedAcknowledgedAt"`

	// Courier dispatch. Courier integration was removed, so this is always
	// null; the field stays because the POS app reads this response.
	Delivery *deliveryDTOType `json:"delivery"`
}

// deliveryDTOType is the shape of the retired courier dispatch. Nothing
// fills it any more; it is kept only so the POS and KDS response contract
// (api/contracts/pos-kds.json) stays the same.
type deliveryDTOType struct {
	Provider string `json:"provider" example:"courier"`
	// dispatching, dispatched, assigned, picked_up, on_the_way, delivered,
	// failed, cancelled or error.
	Status         string  `json:"status" example:"assigned"`
	ProviderStatus *string `json:"providerStatus" example:"NOT_STARTED_YET"`
	ExternalID     *string `json:"externalId" example:"54304791"`
	TrackingURL    *string `json:"trackingUrl"`
	DriverName     *string `json:"driverName"`
	DriverPhone    *string `json:"driverPhone"`
	ExpectedAt     *string `json:"expectedAt"`
	// Why the last dispatch or withdrawal did not go through.
	LastError    *string `json:"lastError"`
	Attempts     int     `json:"attempts"`
	DispatchedAt *string `json:"dispatchedAt"`
	PickedUpAt   *string `json:"pickedUpAt"`
	DeliveredAt  *string `json:"deliveredAt"`
	UpdatedAt    string  `json:"updatedAt"`
}

type billItemDTOType struct {
	OrderItemID string `json:"orderItemId"`
	ProductName string `json:"productName"`
	Quantity    int    `json:"quantity"`
	AmountCents int64  `json:"amountCents"`
}

type billDTOType struct {
	ID          string `json:"id"`
	SplitMode   string `json:"splitMode"`
	Label       string `json:"label"`
	SequenceNo  int    `json:"sequenceNo"`
	AmountCents int64  `json:"amountCents"`
	PaidCents   int64  `json:"paidCents"`
	// What is still owed on this share, so the till does not subtract.
	OutstandingCents int64  `json:"outstandingCents"`
	Status           string `json:"status"`
	// The lines on this bill. Only an item split carries any.
	Items []billItemDTOType `json:"items"`
}

type billPaymentDTOType struct {
	ID            string  `json:"id"`
	BillID        *string `json:"billId"`
	Method        string  `json:"method"`
	AmountCents   int64   `json:"amountCents"`
	TipCents      int64   `json:"tipCents"`
	TenderedCents *int64  `json:"tenderedCents"`
	ChangeCents   int64   `json:"changeCents"`
	Reference     string  `json:"reference"`
	TakenBy       *string `json:"takenBy"`
	CreatedAt     string  `json:"createdAt"`
	// card: stripe (payment link) | manual (own terminal)
	Provider      *string `json:"provider"`
	ProviderRef   *string `json:"providerRef"`
	PaymentLinkID *string `json:"paymentLinkId"`
}

func billDTOList(in []ordersapi.Bill) []billDTOType {
	out := make([]billDTOType, 0, len(in))
	for _, b := range in {
		items := make([]billItemDTOType, 0, len(b.Items))
		for _, it := range b.Items {
			items = append(items, billItemDTOType{
				OrderItemID: it.OrderItemID, ProductName: it.ProductName,
				Quantity: it.Quantity, AmountCents: it.AmountCents,
			})
		}
		outstanding := b.AmountCents - b.PaidCents
		if outstanding < 0 {
			outstanding = 0
		}
		out = append(out, billDTOType{
			ID: b.ID, SplitMode: b.SplitMode, Label: b.Label, SequenceNo: b.SequenceNo,
			AmountCents: b.AmountCents, PaidCents: b.PaidCents, OutstandingCents: outstanding,
			Status: b.Status, Items: items,
		})
	}
	return out
}

func billPaymentDTOList(in []ordersapi.BillPayment) []billPaymentDTOType {
	out := make([]billPaymentDTOType, 0, len(in))
	for _, p := range in {
		out = append(out, billPaymentDTOType{
			ID: p.ID, BillID: p.BillID, Method: p.Method, AmountCents: p.AmountCents,
			TipCents: p.TipCents, TenderedCents: p.TenderedCents, ChangeCents: p.ChangeCents,
			Reference: p.Reference, TakenBy: p.TakenBy, CreatedAt: p.CreatedAt,
			Provider: p.Provider, ProviderRef: p.ProviderRef, PaymentLinkID: p.PaymentLinkID,
		})
	}
	return out
}

type pendingBillDTOType struct {
	ID          string `json:"id"`
	AmountCents int64  `json:"amountCents"`
}

type recipientNameDTOType struct {
	FirstName string  `json:"firstName"`
	LastName  string  `json:"lastName"`
	Phone     *string `json:"phone"`
	// The receiver's own address, distinct from purchaser.email. Null on the
	// majority of orders, which only ever capture the purchaser's.
	Email                *string `json:"email"`
	BusinessName         *string `json:"businessName,omitempty"`
	DeliveryAddressType  *string `json:"deliveryAddressType,omitempty"`
	DeliveryInstructions *string `json:"deliveryInstructions,omitempty"`
	DeliveryPostcode     *string `json:"deliveryPostcode,omitempty"`
	DeliveryState        *string `json:"deliveryState,omitempty"`
	DeliveryCity         *string `json:"deliveryCity,omitempty"`
	DeliveryStreet       *string `json:"deliveryStreet,omitempty"`
	// The recipient's address under the same names the POS request uses.
	// Always present (null when none); the order's delivery address.
	Address  *string `json:"address"`
	City     *string `json:"city"`
	Postcode *string `json:"postcode"`
}

type purchaserDTOType struct {
	FirstName string  `json:"firstName"`
	LastName  string  `json:"lastName"`
	Email     string  `json:"email"`
	Phone     *string `json:"phone"`
}

type customerOrderDTOType struct {
	ID              *string `json:"id"`
	CustomerID      *string `json:"customerId"`
	FirstName       string  `json:"firstName"`
	LastName        string  `json:"lastName"`
	Name            string  `json:"name"`
	Email           string  `json:"email"`
	Phone           *string `json:"phone"`
	MobileNumber    *string `json:"mobileNumber"`
	Address         *string `json:"address"`
	Street          *string `json:"street"`
	City            *string `json:"city"`
	State           *string `json:"state"`
	Postcode        *string `json:"postcode"`
	DOB             *string `json:"dob"`
	AnniversaryDate *string `json:"anniversaryDate"`
}

// buildCustomerDTO builds the customer sub-object, falling back to guest
// fields when the order's purchaser fields are empty (POS guest orders).
func buildCustomerDTO(o *ordersapi.Order) customerOrderDTOType {
	firstName := o.PurchaserFirstName
	lastName := o.PurchaserLastName
	email := o.PurchaserEmail
	phone := o.PurchaserPhone
	var address, city, postcode *string

	// For guest orders, purchaser fields are blank — fall back to guest fields.
	if strings.TrimSpace(firstName) == "" && o.GuestCustomerName != nil {
		// GuestCustomerName may be a full name; split on first space.
		name := strings.TrimSpace(*o.GuestCustomerName)
		if idx := strings.Index(name, " "); idx > 0 {
			firstName = name[:idx]
			lastName = name[idx+1:]
		} else {
			firstName = name
		}
	}
	if strings.TrimSpace(email) == "" && o.GuestCustomerEmail != nil {
		email = *o.GuestCustomerEmail
	}
	if phone == nil && o.GuestCustomerMobile != nil {
		phone = o.GuestCustomerMobile
	}
	if o.GuestCustomerAddress != nil {
		address = o.GuestCustomerAddress
	}
	// The city and postcode typed at the till for this order.
	city, postcode = o.GuestCustomerCity, o.GuestCustomerPostcode

	name := strings.TrimSpace(firstName + " " + lastName)

	return customerOrderDTOType{
		ID:              o.CustomerID,
		CustomerID:      o.CustomerID,
		FirstName:       firstName,
		LastName:        lastName,
		Name:            name,
		Email:           email,
		Phone:           phone,
		MobileNumber:    phone,
		Address:         address,
		Street:          o.GuestCustomerStreet,
		City:            city,
		State:           o.GuestCustomerState,
		Postcode:        postcode,
		DOB:             nil,
		AnniversaryDate: nil,
	}
}

func orderDTO(o *ordersapi.Order) orderDTOType {
	var discountPct *float64
	if o.DiscountPct != nil {
		discountPct = o.DiscountPct
	} else if o.SubtotalCents > 0 && o.DiscountCents > 0 {
		pct := math.Round(float64(o.DiscountCents)*10000/float64(o.SubtotalCents)) / 100
		discountPct = &pct
	}
	// The rate the order was actually taxed at. Only orders stored before
	// the rate was kept fall back to deriving it from the cents.
	taxPct := o.TaxPct
	if taxPct == nil {
		taxable := o.SubtotalCents - o.DiscountCents
		if taxable > 0 && o.TaxCents > 0 {
			pct := math.Round(float64(o.TaxCents)*10000/float64(taxable)) / 100
			taxPct = &pct
		}
	}

	billIDs := o.BillIDs
	if billIDs == nil {
		billIDs = []string{}
	}

	currency := currencyOr(o.Currency)

	out := orderDTOType{
		ID: o.ID, Reference: o.Reference, BillNo: o.BillNo, OrderNo: o.BillNo, BranchID: o.BranchID,
		Status: o.Status, PaymentStatus: o.PaymentStatus, PaymentMethod: o.PaymentMethod, PaymentType: o.PaymentMethod,
		CashCollectedAt: formatTimePtr(o.CashCollectedAt), FulfilmentType: o.FulfilmentType,
		Channel: o.Channel, CustomerID: o.CustomerID,
		Currency: currency, Notes: o.Notes,
		Recipient: recipientNameDTOType{
			FirstName: o.RecipientFirstName, LastName: o.RecipientLastName,
			Phone: o.RecipientPhone, Email: o.RecipientEmail,
			BusinessName: o.BusinessName, DeliveryAddressType: o.DeliveryAddressType,
			DeliveryInstructions: o.DeliveryInstructions, DeliveryPostcode: o.DeliveryPostcode,
			DeliveryState: o.DeliveryState, DeliveryCity: o.DeliveryCity, DeliveryStreet: o.DeliveryStreet,
			Address: o.DeliveryStreet, City: o.DeliveryCity, Postcode: o.DeliveryPostcode,
		},
		GiftMessage: o.GiftMessage, GiftReason: o.GiftReason, IsGift: o.IsGift,
		Purchaser:            purchaserDTOType{FirstName: o.PurchaserFirstName, LastName: o.PurchaserLastName, Email: o.PurchaserEmail, Phone: o.PurchaserPhone},
		Customer:             buildCustomerDTO(o),
		DeliveryInstructions: o.DeliveryInstructions,
		BusinessName:         o.BusinessName, DeliveryAddressType: o.DeliveryAddressType,
		PhotoOnDelivery: o.PhotoOnDelivery, SurpriseDelivery: o.SurpriseDelivery,
		ScheduledDate: o.ScheduledDate.Format(dateFmt), SlotStart: o.SlotStart, SlotEnd: o.SlotEnd,
		Summary: priceSummaryDTO(&ordersapi.PriceSummary{
			SubtotalCents: o.SubtotalCents, DiscountCents: o.DiscountCents, DiscountPct: discountPct,
			DeliveryFeeCents: o.DeliveryFeeCents, TaxCents: o.TaxCents, TaxPct: taxPct,
			ServiceChargeCents: o.ServiceChargeCents, TipCents: o.TipCents, TotalCents: o.TotalCents,
			Currency: currency,
		}),
		TotalCents:         o.TotalCents,
		ServiceChargeCents: o.ServiceChargeCents,
		TipCents:           o.TipCents,
		DiscountCode:       o.DiscountCode,
		BillIDs:            billIDs,
		PendingBillIDs:     pendingBillDTOList(o.PendingBillIDs),
		TotalBillsCount:    o.TotalBillsCount,
		SplitMode:          o.SplitMode,
		Bills:              billDTOList(o.Bills),
		Payments:           billPaymentDTOList(o.Payments),
		CanAmend:           ordersapi.IsAmendable(o.Status), CanCancel: ordersapi.IsAmendable(o.Status),
		CreatedAt:             o.CreatedAt.Format(rfc3339),
		StartTime:             orderStartTime(o),
		EndTime:               orderEndTime(o),
		TotalDuration:         totalDuration(o),
		SettledBy:             settledDisplay(o),
		SettledByName:         o.SettledByName,
		SettledByID:           o.SettledBy,
		ReturnedBy:            returnedDisplay(o),
		ReturnedByName:        o.ReturnedByName,
		ReturnedByID:          o.ReturnedBy,
		settleCancelDTO:       settleCancel(o),
		BillerID:              o.BillerID,
		BillerName:            o.BillerName,
		UpdatedBillerID:       o.UpdatedBillerID,
		UpdatedBillerName:     o.UpdatedBillerName,
		GuestCustomerName:     o.GuestCustomerName,
		GuestCustomerMobile:   o.GuestCustomerMobile,
		GuestCustomerAddress:  o.GuestCustomerAddress,
		GuestCustomerEmail:    o.GuestCustomerEmail,
		GuestCustomerCity:     o.GuestCustomerCity,
		GuestCustomerPostcode: o.GuestCustomerPostcode,
		OrderDeliverDate:      o.OrderDeliverDate,
		OrderDeliverTime:      o.OrderDeliverTime,

		KotStatus:             orDefaultStr(o.KOTStatus, ordersapi.KOTQueue),
		KotStartedAt:          fmtTimePtr(o.KOTStartedAt),
		KotReadyAt:            fmtTimePtr(o.KOTReadyAt),
		KotCompletedAt:        fmtTimePtr(o.KOTCompletedAt),
		IsAmended:             o.IsAmended,
		AmendedAt:             fmtTimePtr(o.AmendedAt),
		AmendedAcknowledged:   o.AmendedAcknowledged,
		AmendedAcknowledgedAt: fmtTimePtr(o.AmendedAcknowledgedAt),
	}
	if o.DeliveryStreet != nil {
		out.DeliveryAddress = &addressDTOType{Street: o.DeliveryStreet, City: o.DeliveryCity, State: o.DeliveryState, Postcode: o.DeliveryPostcode}
	}
	for _, i := range o.Items {
		out.Items = append(out.Items, orderItemDTO(&i))
	}
	out.ReturnedQuantity, out.ReturnedTotalCents = returnedTotals(o.Items)
	out.ReturnSummary = orderReturnSummaryDTO(o)
	out.Returns = orderReturnDTOList(o.Returns)
	if out.Items == nil {
		out.Items = []orderItemDTOType{}
	}
	out.Timeline = []timelineEventDTOType{}
	for _, e := range o.Timeline {
		actor := e.ActorType
		out.Timeline = append(out.Timeline, timelineEventDTOType{Status: e.Status, At: e.CreatedAt.Format(rfc3339), Actor: actor, Note: derefStr(e.Note)})
	}
	return out
}

// orderSummaryDTO is the lighter shape for list views (ADM-01, account/orders).
type orderSummaryDTOType struct {
	ID             string  `json:"id"`
	Reference      string  `json:"reference"`
	BillNo         *int    `json:"billNo"`
	OrderNo        *int    `json:"orderNo"`
	BranchID       string  `json:"branchId"`
	Status         string  `json:"status"`
	FulfilmentType string  `json:"fulfilmentType"`
	RecipientName  string  `json:"recipientName"`
	PurchaserName  string  `json:"purchaserName"`
	PurchaserPhone *string `json:"purchaserPhone"`
	ScheduledDate  string  `json:"scheduledDate"`
	SlotStart      string  `json:"slotStart"`
	SlotEnd        string  `json:"slotEnd"`
	TotalCents     int64   `json:"totalCents"`
	// What this order counts towards the list's total: 0 for a cancelled,
	// fully returned or refunded order, otherwise totalCents (already net of
	// any items returned). Summing this over the rows gives stats.totalPrice.
	NetTotalCents      int64 `json:"netTotalCents"`
	ServiceChargeCents int64 `json:"serviceChargeCents"`
	TipCents           int64 `json:"tipCents"`
	// Units handed back and what they were sold for; zero with no returns.
	ReturnedQuantity   int   `json:"returnedQuantity"`
	ReturnedTotalCents int64 `json:"returnedTotalCents"`
	// Discount, tax and refund over every return on the order.
	ReturnSummary returnChargesDTOType `json:"returnSummary"`
	// Payment state on the list, not just the detail: a cash-on-delivery
	// order still owing money is the one an operator most needs to spot
	// while scanning, and making them open each order to find out defeats
	// the purpose of the list.
	PaymentStatus string             `json:"paymentStatus"`
	PaymentMethod string             `json:"paymentMethod"`
	PaymentType   string             `json:"paymentType"`
	Channel       string             `json:"channel"`
	ItemCount     int                `json:"itemCount"`
	Items         []orderItemDTOType `json:"items"`
	PlacedAt      string             `json:"placedAt"`
	// The coupon used, on the summary as well as the detail, so an export of
	// the order list can say which offer a discount came from without
	// fetching every order one at a time.
	DiscountCode    *string              `json:"discountCode"`
	CustomerID      *string              `json:"customerId"`
	Customer        customerOrderDTOType `json:"customer"`
	BillIDs         []string             `json:"billIds"`
	PendingBillIDs  []pendingBillDTOType `json:"pendingBillIds"`
	TotalBillsCount int                  `json:"totalBillsCount"`
	SplitMode       *string              `json:"splitMode"`

	BillerID          *string `json:"billerId"`
	BillerName        *string `json:"billerName"`
	UpdatedBillerID   *string `json:"updatedBillerId"`
	UpdatedBillerName *string `json:"updatedBillerName"`

	StartTime      string  `json:"startTime"`
	EndTime        string  `json:"endTime"`
	TotalDuration  *int    `json:"totalDuration"`
	SettledBy      *string `json:"settledBy"`
	SettledByName  *string `json:"settledByName"`
	SettledByID    *string `json:"settledById"`
	ReturnedBy     *string `json:"returnedBy"`
	ReturnedByName *string `json:"returnedByName"`
	ReturnedByID   *string `json:"returnedById"`
	// When it was fully paid, and who cancelled it, when and why.
	settleCancelDTO

	GuestCustomerName     *string `json:"guestCustomerName" example:"Jane Doe"`
	GuestCustomerMobile   *string `json:"guestCustomerMobile" example:"+1234567890"`
	GuestCustomerAddress  *string `json:"guestCustomerAddress" example:"123 Flower Street"`
	GuestCustomerEmail    *string `json:"guestCustomerEmail" example:"guest@example.com"`
	GuestCustomerCity     *string `json:"guestCustomerCity" example:"Edison"`
	GuestCustomerPostcode *string `json:"guestCustomerPostcode" example:"08817"`
	OrderDeliverDate      *string `json:"orderDeliverDate" example:"2026-09-07"`
	OrderDeliverTime      *string `json:"orderDeliverTime" example:"14:30:00"`

	// KDS
	KotStatus           string `json:"kotStatus" example:"queue"`
	IsAmended           bool   `json:"isAmended"`
	AmendedAcknowledged bool   `json:"amendedAcknowledged"`
}

func pendingBillDTOList(in []ordersapi.PendingBillRef) []pendingBillDTOType {
	out := make([]pendingBillDTOType, 0, len(in))
	for _, pb := range in {
		out = append(out, pendingBillDTOType{
			ID:          pb.ID,
			AmountCents: pb.AmountCents,
		})
	}
	return out
}

// returnedTotals is what came back on an order: units, and what those units
// were sold for — the same per-line rate the return itself refunded
// (unit price × returned quantity, see store.ReturnPOSOrder).
func returnedTotals(items []ordersapi.OrderItem) (qty int, cents int64) {
	for _, i := range items {
		if i.ReturnedQuantity > 0 {
			qty += i.ReturnedQuantity
			cents += int64(i.ReturnedQuantity) * i.UnitPriceCents
		}
	}
	return qty, cents
}

func orderSummaryDTO(o *ordersapi.Order) orderSummaryDTOType {
	// ItemCount comes from the list query's subquery (o.ItemCount); Items
	// is intentionally left empty for list views, so len(o.Items) would
	// always read 0 here — see the ItemCount field doc comment on ordersapi.Order.
	count := o.ItemCount
	if count == 0 && len(o.Items) > 0 {
		count = len(o.Items)
	}
	billIDs := o.BillIDs
	if billIDs == nil {
		billIDs = []string{}
	}
	items := make([]orderItemDTOType, 0, len(o.Items))
	for i := range o.Items {
		items = append(items, orderItemDTO(&o.Items[i]))
	}
	returnedQty, returnedCents := returnedTotals(o.Items)
	return orderSummaryDTOType{
		ID: o.ID, Reference: o.Reference, BillNo: o.BillNo, OrderNo: o.BillNo, BranchID: o.BranchID, Status: o.Status, FulfilmentType: o.FulfilmentType,
		RecipientName: o.RecipientFirstName + " " + o.RecipientLastName,
		PurchaserName: o.PurchaserFirstName + " " + o.PurchaserLastName, PurchaserPhone: o.PurchaserPhone,
		ScheduledDate: o.ScheduledDate.Format(dateFmt), SlotStart: o.SlotStart, SlotEnd: o.SlotEnd,
		TotalCents: o.TotalCents, NetTotalCents: netTotalCents(o), ServiceChargeCents: o.ServiceChargeCents, TipCents: o.TipCents,
		ReturnedQuantity: returnedQty, ReturnedTotalCents: returnedCents,
		ReturnSummary: orderReturnSummaryDTO(o),
		PaymentStatus: o.PaymentStatus, PaymentMethod: o.PaymentMethod,
		PaymentType: o.PaymentMethod, Channel: o.Channel, ItemCount: count, Items: items, PlacedAt: o.CreatedAt.Format(rfc3339),
		DiscountCode:          o.DiscountCode,
		CustomerID:            o.CustomerID,
		Customer:              buildCustomerDTO(o),
		BillIDs:               billIDs,
		PendingBillIDs:        pendingBillDTOList(o.PendingBillIDs),
		TotalBillsCount:       o.TotalBillsCount,
		SplitMode:             o.SplitMode,
		BillerID:              o.BillerID,
		BillerName:            o.BillerName,
		UpdatedBillerID:       o.UpdatedBillerID,
		UpdatedBillerName:     o.UpdatedBillerName,
		StartTime:             orderStartTime(o),
		EndTime:               orderEndTime(o),
		TotalDuration:         totalDuration(o),
		SettledBy:             settledDisplay(o),
		SettledByName:         o.SettledByName,
		SettledByID:           o.SettledBy,
		ReturnedBy:            returnedDisplay(o),
		ReturnedByName:        o.ReturnedByName,
		ReturnedByID:          o.ReturnedBy,
		settleCancelDTO:       settleCancel(o),
		GuestCustomerName:     o.GuestCustomerName,
		GuestCustomerMobile:   o.GuestCustomerMobile,
		GuestCustomerAddress:  o.GuestCustomerAddress,
		GuestCustomerEmail:    o.GuestCustomerEmail,
		GuestCustomerCity:     o.GuestCustomerCity,
		GuestCustomerPostcode: o.GuestCustomerPostcode,
		OrderDeliverDate:      o.OrderDeliverDate,
		OrderDeliverTime:      o.OrderDeliverTime,
		KotStatus:             orDefaultStr(o.KOTStatus, ordersapi.KOTQueue),
		IsAmended:             o.IsAmended,
		AmendedAcknowledged:   o.AmendedAcknowledged,
	}
}

// --- Public tracking DTO (SF-32): deliberately minimal ------------------------

type trackingDTOType struct {
	Reference      string                 `json:"reference"`
	Status         string                 `json:"status"`
	Recipient      string                 `json:"recipient"`
	FulfilmentType string                 `json:"fulfilmentType"`
	ScheduledDate  string                 `json:"scheduledDate"`
	SlotStart      string                 `json:"slotStart"`
	SlotEnd        string                 `json:"slotEnd"`
	Timeline       []timelineEventDTOType `json:"timeline"`
}

func trackingDTO(o *ordersapi.Order) trackingDTOType {
	out := trackingDTOType{
		Reference: o.Reference, Status: o.Status, Recipient: o.RecipientFirstName + " " + initialOf(o.RecipientLastName),
		FulfilmentType: o.FulfilmentType, ScheduledDate: o.ScheduledDate.Format(dateFmt), SlotStart: o.SlotStart, SlotEnd: o.SlotEnd,
	}
	for _, e := range o.Timeline {
		out.Timeline = append(out.Timeline, timelineEventDTOType{Status: e.Status, At: e.CreatedAt.Format(rfc3339)})
	}
	return out
}

func initialOf(s string) string {
	if s == "" {
		return ""
	}
	return string(s[0]) + "."
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// formatTimePtr renders an optional timestamp, keeping it absent from the JSON
// rather than emitting a zero time — "never collected" and "collected at the
// epoch" must not look the same to a client.
func formatTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(rfc3339)
	return &s
}

// fmtTimePtr renders an optional timestamp, or null when there isn't one.
func fmtTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(rfc3339)
	return &s
}

// taxLabel falls back to something a customer can read. An outlet that has
// never been given a tax name should still not show an empty row.
func taxLabel(name string) string {
	if strings.TrimSpace(name) == "" {
		return "Sales Tax"
	}
	return name
}

// orderStartTime is the stored start_time; created_at only for rows that
// predate the column and were not backfilled.
func orderStartTime(o *ordersapi.Order) string {
	if o.StartTime != nil {
		return o.StartTime.Format(rfc3339)
	}
	return o.CreatedAt.Format(rfc3339)
}

// orderEndTime is the stored end_time, or empty while the order is still
// open — no longer updated_at, which moved on every edit.
func orderEndTime(o *ordersapi.Order) string {
	if o.EndTime != nil {
		return o.EndTime.Format(rfc3339)
	}
	return ""
}

func totalDuration(o *ordersapi.Order) *int {
	if o.TotalDurationSecs != nil {
		return o.TotalDurationSecs
	}
	if o.PaymentStatus == "captured" || o.Status == "fulfilled" || o.Status == "settled" {
		dur := int(o.UpdatedAt.Sub(o.CreatedAt).Seconds())
		if dur < 0 {
			dur = 0
		}
		return &dur
	}
	return nil
}

func settledDisplay(o *ordersapi.Order) *string {
	if o.SettledByName != nil && strings.TrimSpace(*o.SettledByName) != "" {
		return o.SettledByName
	}
	return o.SettledBy
}

func returnedDisplay(o *ordersapi.Order) *string {
	if o.ReturnedByName != nil && strings.TrimSpace(*o.ReturnedByName) != "" {
		return o.ReturnedByName
	}
	return o.ReturnedBy
}

// settleCancelDTO is when an order was settled and, for a cancelled order,
// who cancelled it, when and why. Embedded in the order and list DTOs.
type settleCancelDTO struct {
	// When the order was fully paid. Null while money is owed.
	SettledAt *string `json:"settledAt" example:"2026-09-28T10:45:10Z"`
	// Null unless the order is cancelled.
	CancelledAt *string `json:"cancelledAt" example:"2026-09-28T10:40:00Z"`
	// The staff member's or customer's id; null when the system cancelled it.
	CancelledByID *string `json:"cancelledById"`
	// Their name (email when they have none).
	CancelledByName *string `json:"cancelledByName" example:"Olivia Carter"`
	// staff, customer or system.
	CancelledByType *string `json:"cancelledByType" example:"staff"`
	// Name for display: cancelledByName, else "System" / "Customer".
	CancelledBy  *string `json:"cancelledBy" example:"Olivia Carter"`
	CancelReason *string `json:"cancelReason" example:"Customer changed their mind"`
}

func settleCancel(o *ordersapi.Order) settleCancelDTO {
	out := settleCancelDTO{SettledAt: fmtTimePtr(o.SettledAt)}
	c := o.Cancellation
	if c == nil {
		return out
	}
	// Read back from JSON in UTC; shown in the same zone as every other
	// timestamp on the order.
	at := c.At.In(time.Local).Format(rfc3339)
	out.CancelledAt = &at
	out.CancelledByID = c.ByID
	out.CancelledByName = c.ByName
	if c.ByType != "" {
		t := c.ByType
		out.CancelledByType = &t
	}
	display := c.ByName
	if display == nil || strings.TrimSpace(*display) == "" {
		var d string
		switch c.ByType {
		case "customer":
			d = "Customer"
		case "staff":
			d = "Staff"
		default:
			d = "System"
		}
		display = &d
	}
	out.CancelledBy = display
	if c.Reason != nil && strings.TrimSpace(*c.Reason) != "" {
		out.CancelReason = c.Reason
	}
	return out
}

// --- Returns: tax and discount -------------------------------------------------

// returnChargesDTOType is the money on a return, or on every return of an
// order: refundAmountCents = grossAmountCents - discountCents + taxCents.
type returnChargesDTOType struct {
	GrossAmountCents  int64    `json:"grossAmountCents"`
	DiscountPct       *float64 `json:"discountPct"`
	DiscountCents     int64    `json:"discountCents"`
	TaxPct            *float64 `json:"taxPct"`
	TaxCents          int64    `json:"taxCents"`
	RefundAmountCents int64    `json:"refundAmountCents"`
}

func returnChargesDTO(c ordersapi.ReturnCharges) returnChargesDTOType {
	return returnChargesDTOType{
		GrossAmountCents: c.GrossCents, DiscountPct: c.DiscountPct, DiscountCents: c.DiscountCents,
		TaxPct: c.TaxPct, TaxCents: c.TaxCents, RefundAmountCents: c.RefundCents,
	}
}

// orderReturnSummaryDTO is the order-based total over every return.
func orderReturnSummaryDTO(o *ordersapi.Order) returnChargesDTOType {
	return returnChargesDTO(ordersapi.ReturnCharges{
		GrossCents: o.ReturnAmountCents, DiscountPct: o.ReturnDiscountPct, DiscountCents: o.ReturnDiscountCents,
		TaxPct: o.ReturnTaxPct, TaxCents: o.ReturnTaxCents, RefundCents: o.ReturnTotalCents,
	})
}

type orderReturnItemDTOType struct {
	OrderItemID string `json:"orderItemId"`
	ProductName string `json:"productName"`
	Quantity    int    `json:"quantity"`
	// Gross: unit price × quantity returned.
	AmountCents int64 `json:"amountCents" example:"2000"`
	// This item's own return discount (its line discount prorated by the
	// quantity returned, plus its share of the order discount) and the tax on
	// what is left. refundAmountCents = amountCents - discountCents + taxCents.
	DiscountPct       *float64 `json:"discountPct" example:"10"`
	DiscountCents     int64    `json:"discountCents" example:"200"`
	TaxPct            *float64 `json:"taxPct" example:"6.625"`
	TaxCents          int64    `json:"taxCents" example:"119"`
	RefundAmountCents int64    `json:"refundAmountCents" example:"1919"`
}

// orderReturnDTOType is one return as recorded (return based).
type orderReturnDTOType struct {
	ID             string  `json:"id"`
	Reason         *string `json:"reason"`
	IsFullReturn   bool    `json:"isFullReturn"`
	ReturnedByID   *string `json:"returnedById"`
	ReturnedByName *string `json:"returnedByName"`
	ReturnedAt     string  `json:"returnedAt"`
	returnChargesDTOType
	Items []orderReturnItemDTOType `json:"items"`
}

func orderReturnDTOList(in []ordersapi.OrderReturn) []orderReturnDTOType {
	out := make([]orderReturnDTOType, 0, len(in))
	for _, r := range in {
		items := make([]orderReturnItemDTOType, 0, len(r.Items))
		for _, it := range r.Items {
			items = append(items, orderReturnItemDTOType{
				OrderItemID: it.OrderItemID, ProductName: it.ProductName,
				Quantity: it.Quantity, AmountCents: it.AmountCents,
				DiscountPct: it.DiscountPct, DiscountCents: it.DiscountCents,
				TaxPct: it.TaxPct, TaxCents: it.TaxCents, RefundAmountCents: it.RefundCents,
			})
		}
		out = append(out, orderReturnDTOType{
			ID: r.ID, Reason: r.Reason, IsFullReturn: r.IsFullReturn,
			ReturnedByID: r.ReturnedBy, ReturnedByName: r.ReturnedByName,
			ReturnedAt:           r.CreatedAt.Format(rfc3339),
			returnChargesDTOType: returnChargesDTO(r.ReturnCharges),
			Items:                items,
		})
	}
	return out
}

// netTotalCents is what an order counts towards a list's revenue total.
func netTotalCents(o *ordersapi.Order) int64 {
	switch strings.ToLower(o.Status) {
	case ordersapi.StatusCancelled, ordersapi.StatusReturned, ordersapi.StatusRefunded:
		return 0
	}
	return o.TotalCents
}
