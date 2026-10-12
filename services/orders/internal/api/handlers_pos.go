package api

import (
	"net/http"
	"strings"
	"time"

	"bakehouse/pkg/httpx"
	"bakehouse/pkg/timex"
	"bakehouse/services/orders/internal/domain"
	"bakehouse/services/orders/ordersapi"
)

/*
 * The till's HTTP surface.
 *
 * Everything here writes to the same tables the website and the admin console
 * read, so an order rung up at a counter appears on the kitchen screen, in the
 * admin list and on the customer's record without anything having to sync.
 */

type posItemRequest struct {
	// Set for a line the order already has, so an update changes it rather
	// than adding a second copy. Leave it out for a new line.
	ID string `json:"id,omitempty"`
	// Which round this line is on. Omit and it goes on the next one.
	BatchNo int `json:"batchNo,omitempty"`
	// When the till sent it to the kitchen.
	SendKotTime         *timex.FlexTime `json:"sendKotTime,omitempty" swaggertype:"string" example:"2026-09-18T10:53:00Z"`
	CancelTime          *timex.FlexTime `json:"cancelTime,omitempty" swaggertype:"string" example:"2026-09-18T10:53:00Z"`
	ProductionStartedAt *timex.FlexTime `json:"productionStartedAt,omitempty" swaggertype:"string" example:"2026-09-18T10:53:00Z"`
	ProductionDoneAt    *timex.FlexTime `json:"productionDoneAt,omitempty" swaggertype:"string" example:"2026-09-18T10:53:00Z"`
	// pending, in_progress, done, cancel, cancelled or returned.
	Status      string `json:"status,omitempty"`
	VariantID   string `json:"variantId,omitempty"`
	VariantName string `json:"variantName,omitempty"`

	ProductID   string `json:"productId"`
	ProductName string `json:"productName"`
	Quantity    int    `json:"quantity"`

	UnitPriceCents int64                           `json:"unitPriceCents"`
	LineTotalCents int64                           `json:"lineTotalCents"`
	Comments       string                          `json:"comments,omitempty"`
	Flavours       []ordersapi.SelectedModifierRef `json:"selectedFlavours,omitempty"`
	Addons         []ordersapi.SelectedModifierRef `json:"selectedAddons,omitempty"`

	DiscountPct       *float64 `json:"discountPct,omitempty"`
	DiscountCents     int64    `json:"discountCents,omitempty"`
	ItemDiscountPct   *float64 `json:"itemDiscountPct,omitempty"`
	ItemDiscountCents *int64   `json:"itemDiscountCents,omitempty"`

	// KDS: the line's kitchen status. Omit on a new line (it joins the queue)
	// and on an existing line to leave it as it is.
	KotStatus string `json:"kotStatus,omitempty" example:"queue"`

	// Gift message and occasion for this line. On an update: key omitted keeps
	// the stored value; null or "" clears it; a string replaces it.
	GiftMessage ordersapi.OptionalString `json:"giftMessage,omitempty" swaggertype:"string" example:"Happy birthday!"`
	GiftReason  ordersapi.OptionalString `json:"giftReason,omitempty" swaggertype:"string" example:"Birthday"`
}

// giftPatch turns a three-state JSON field into the two-state pointer the
// domain and store already understand: nil keeps the stored value, a pointer
// to "" clears it, any other string replaces it.
func giftPatch(o ordersapi.OptionalString) *string {
	if !o.Set {
		return nil
	}
	if o.Value == nil {
		empty := ""
		return &empty
	}
	return o.Value
}

func (r posItemRequest) toItem() ordersapi.POSItem {
	price := r.UnitPriceCents
	total := r.LineTotalCents
	if total == 0 && price > 0 {
		total = price * int64(r.Quantity)
	}
	if price == 0 && total > 0 && r.Quantity > 0 {
		price = total / int64(r.Quantity)
	}

	discountPct := r.ItemDiscountPct
	if discountPct == nil {
		discountPct = r.DiscountPct
	}

	discountCents := r.DiscountCents
	if r.ItemDiscountCents != nil {
		discountCents = *r.ItemDiscountCents
	}

	status := strings.TrimSpace(r.Status)
	if status != "" {
		status = ordersapi.NormalizeItemStatus(status)
	}

	return ordersapi.POSItem{
		ID: r.ID, BatchNo: r.BatchNo, SendKotTime: r.SendKotTime.Ptr(), Status: status,
		ProductID: strings.TrimSpace(r.ProductID), ProductName: strings.TrimSpace(r.ProductName),
		VariantID: r.VariantID, VariantName: r.VariantName,
		Quantity: r.Quantity, UnitPriceCents: price, LineTotalCents: total,
		Flavours: r.Flavours, Addons: r.Addons,
		DiscountPct: discountPct, DiscountCents: discountCents,
		Comments:            strings.TrimSpace(r.Comments),
		CancelTime:          r.CancelTime.Ptr(),
		ProductionStartedAt: r.ProductionStartedAt.Ptr(),
		ProductionDoneAt:    r.ProductionDoneAt.Ptr(),
		KOTStatus:           strings.TrimSpace(r.KotStatus),
		GiftMessage:         giftPatch(r.GiftMessage),
		GiftReason:          giftPatch(r.GiftReason),
	}
}

func toItems(in []posItemRequest) []ordersapi.POSItem {
	out := make([]ordersapi.POSItem, 0, len(in))
	for _, it := range in {
		out = append(out, it.toItem())
	}
	return out
}

type posChargesRequest struct {
	DiscountPct        *float64 `json:"discountPct,omitempty"`
	DiscountCents      int64    `json:"discountCents,omitempty"`
	TaxPct             *float64 `json:"taxPct,omitempty"`
	TaxCents           int64    `json:"taxCents,omitempty"`
	DeliveryFeeCents   int64    `json:"deliveryFeeCents,omitempty"`
	ServiceChargeCents int64    `json:"serviceChargeCents,omitempty"`
	TipCents           int64    `json:"tipCents,omitempty"`
}

func (c posChargesRequest) toTotals() (ordersapi.OrderTotals, *float64) {
	return ordersapi.OrderTotals{
		DiscountPct: c.DiscountPct, DiscountCents: c.DiscountCents,
		TaxCents: c.TaxCents, DeliveryFeeCents: c.DeliveryFeeCents,
		ServiceChargeCents: c.ServiceChargeCents, TipCents: c.TipCents,
	}, c.TaxPct
}

// posCustomerRequest is the customer on a till order. On create it is who
// the order is rung up against; on update, with a customerId, it is written
// onto that customer's record.
type posCustomerRequest struct {
	ID        string `json:"id,omitempty" example:"c0a80123-4567-89ab-cdef-0123456789ab"`
	FirstName string `json:"firstName" example:"Jane"`
	LastName  string `json:"lastName" example:"Doe"`
	Phone     string `json:"phone,omitempty" example:"+1234567890"`
	Email     string `json:"email,omitempty" example:"guest@example.com"`
	Address   string `json:"address,omitempty" example:"123 Flower Street"`
	Street    string `json:"street,omitempty" example:"123 Flower Street"`
	City      string `json:"city,omitempty" example:"Edison"`
	State     string `json:"state,omitempty" example:"NJ"`
	Postcode  string `json:"postcode,omitempty" example:"08817"`
}

func (c *posCustomerRequest) toDomain() *domain.POSCustomer {
	if c == nil {
		return nil
	}
	return &domain.POSCustomer{
		FirstName: c.FirstName, LastName: c.LastName, Phone: c.Phone, Email: c.Email,
		Address: c.Address, Street: c.Street, City: c.City, State: c.State, Postcode: c.Postcode,
	}
}

// posRecipientRequest is the person the order is for — the storefront's
// recipient fields. On update an omitted field keeps the order's value; an
// empty phone or email clears it.
type posRecipientRequest struct {
	FirstName *string `json:"firstName,omitempty" example:"Priya"`
	LastName  *string `json:"lastName,omitempty" example:"Raman"`
	Phone     *string `json:"phone,omitempty" example:"+17325550123"`
	Email     *string `json:"email,omitempty" example:"priya@example.com"`
	// The recipient's address — saved as the order's delivery address.
	Address  *string `json:"address,omitempty" example:"12 Elm Street"`
	City     *string `json:"city,omitempty" example:"Edison"`
	Postcode *string `json:"postcode,omitempty" example:"08820"`
}

func (r *posRecipientRequest) toDomain() *ordersapi.POSRecipient {
	if r == nil {
		return nil
	}
	return &ordersapi.POSRecipient{
		FirstName: r.FirstName, LastName: r.LastName, Phone: r.Phone, Email: r.Email,
		Address: r.Address, City: r.City, Postcode: r.Postcode,
	}
}

type posOrderRequest struct {
	BranchID       string              `json:"branchId" example:"br_123"`
	FulfilmentType string              `json:"fulfilmentType,omitempty" example:"pickup"`
	ScheduledDate  *string             `json:"scheduledDate,omitempty" example:"2026-09-07"`
	SlotStart      *string             `json:"slotStart,omitempty" example:"14:00:00"`
	SlotEnd        *string             `json:"slotEnd,omitempty" example:"14:30:00"`
	Status         string              `json:"status,omitempty" example:"confirmed"`
	StatusAt       *string             `json:"statusAt,omitempty" example:"2026-09-16T10:33:52Z"`
	CustomerID     *string             `json:"customerId,omitempty" example:"c0a80123-4567-89ab-cdef-0123456789ab"`
	Customer       *posCustomerRequest `json:"customer,omitempty"`
	// Who the order is for, as on the storefront. Omit and the recipient is
	// the customer (the purchaser).
	Recipient *posRecipientRequest `json:"recipient,omitempty"`
	// Gift card message and occasion ("Birthday", "Sympathy"…), as on the storefront.
	GiftMessage *string `json:"giftMessage,omitempty" example:"Happy birthday, Amma!"`
	GiftReason  *string `json:"giftReason,omitempty" example:"Birthday"`
	// "Send as a gift". Omit to keep the default.
	IsGift *bool `json:"isGift,omitempty" example:"true"`

	GuestCustomerName     *string `json:"guestCustomerName,omitempty" example:"Jane Doe"`
	GuestCustomerMobile   *string `json:"guestCustomerMobile,omitempty" example:"+1234567890"`
	GuestCustomerAddress  *string `json:"guestCustomerAddress,omitempty" example:"123 Flower Street"`
	GuestCustomerCity     *string `json:"guestCustomerCity,omitempty" example:"Edison"`
	GuestCustomerPostcode *string `json:"guestCustomerPostcode,omitempty" example:"08817"`
	GuestCustomerEmail    *string `json:"guestCustomerEmail,omitempty" example:"guest@example.com"`
	OrderDeliverDate      *string `json:"orderDeliverDate,omitempty" example:"2026-09-07"`
	OrderDeliverTime      *string `json:"orderDeliverTime,omitempty" example:"14:30:00"`

	Items []posItemRequest `json:"items"`
	posChargesRequest
	Notes string `json:"notes,omitempty"`

	// KDS: the order's kitchen status at creation. Omit and it joins the
	// queue.
	KotStatus string `json:"kotStatus,omitempty" example:"queue"`
	// KDS: flag the new order "Amended" on the kitchen display.
	MarkAmended bool `json:"markAmended,omitempty"`
}

// @Summary		Send a counter order to the kitchen
// @Description	Places the order and prints batch 1. There is no cart to keep — the screen is the cart. The purchaser is the customer (customerId, the customer object, or the guest details). recipient (firstName, lastName, phone, email — as on the storefront) sets who the order is for; omit it and the recipient is the customer.
// @Tags			pos
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body	posOrderRequest	true	"Request body"
// @Success		201	{object}	httpx.Envelope{data=orderDTOType}
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/pos/orders [post]
func (h *Handler) CreatePOSOrder(w http.ResponseWriter, r *http.Request) {
	var req posOrderRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	scheduledDate := parsePOSDate(req.ScheduledDate)
	slotStart := parsePOSTime(req.SlotStart)
	slotEnd := parsePOSTime(req.SlotEnd)
	if slotEnd == nil && slotStart != nil {
		if t, err := time.Parse("15:04:05", *slotStart); err == nil {
			end := t.Add(30 * time.Minute).Format("15:04:05")
			slotEnd = &end
		}
	}

	totals, reqTaxPct := req.posChargesRequest.toTotals()

	customerID := req.CustomerID
	if (customerID == nil || *customerID == "") && req.Customer != nil && req.Customer.ID != "" {
		customerID = &req.Customer.ID
	}

	in := domain.POSOrderRequest{
		BranchID:              req.BranchID,
		FulfilmentType:        req.FulfilmentType,
		ScheduledDate:         scheduledDate,
		SlotStart:             slotStart,
		SlotEnd:               slotEnd,
		CustomerID:            customerID,
		GuestCustomerName:     req.GuestCustomerName,
		GuestCustomerMobile:   req.GuestCustomerMobile,
		GuestCustomerAddress:  req.GuestCustomerAddress,
		GuestCustomerCity:     req.GuestCustomerCity,
		GuestCustomerPostcode: req.GuestCustomerPostcode,
		GuestCustomerEmail:    req.GuestCustomerEmail,
		OrderDeliverDate:      parsePOSDateStr(req.OrderDeliverDate),
		OrderDeliverTime:      parsePOSTime(req.OrderDeliverTime),
		Items:                 toItems(req.Items),
		DiscountPct:           totals.DiscountPct,
		DiscountCents:         totals.DiscountCents,
		TaxPct:                reqTaxPct,
		TaxCents:              totals.TaxCents,
		DeliveryFeeCents:      totals.DeliveryFeeCents,
		ServiceChargeCents:    totals.ServiceChargeCents,
		TipCents:              totals.TipCents,
		Notes:                 req.Notes,
		Status:                req.Status,
		Customer:              req.Customer.toDomain(),
		Recipient:             req.Recipient.toDomain(),
		GiftMessage:           req.GiftMessage,
		GiftReason:            req.GiftReason,
		IsGift:                req.IsGift,
		KOTStatus:             strings.TrimSpace(req.KotStatus),
		MarkAmended:           req.MarkAmended,
	}

	r, err := httpx.WithEventTime(r, "statusAt", req.StatusAt)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	order, err := h.svc.CreatePOSOrder(r.Context(), staffClaims(r), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	dto := orderDTO(order)
	h.enrichCustomerDTO(r.Context(), &dto.Customer, order.CustomerID)
	httpx.Created(w, dto)
}
