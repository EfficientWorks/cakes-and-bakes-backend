package api

import (
	"math"
	"strings"

	"bakehouse/services/orders/ordersapi"
)

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
