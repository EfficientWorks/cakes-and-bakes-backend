package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/httpx"
	"bakehouse/pkg/timex"
	"bakehouse/services/franchise/franchiseapi"
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

// Pointers, not values: a till adding a batch usually sends only the new
// lines, and an absent charge must leave the order's own alone rather than
// zero it. See ordersapi.OrderTotalsPatch.
type posChargesPatchRequest struct {
	DiscountPct   *float64 `json:"discountPct,omitempty"`
	DiscountCents *int64   `json:"discountCents,omitempty"`
	TaxCents      *int64   `json:"taxCents,omitempty"`
	// Change the rate the tax is computed at. Omit to keep the order's rate.
	TaxPct             *float64 `json:"taxPct,omitempty"`
	DeliveryFeeCents   *int64   `json:"deliveryFeeCents,omitempty"`
	ServiceChargeCents *int64   `json:"serviceChargeCents,omitempty"`
	TipCents           *int64   `json:"tipCents,omitempty"`
}

func (c posChargesPatchRequest) toPatch() ordersapi.OrderTotalsPatch {
	return ordersapi.OrderTotalsPatch{
		DiscountPct: c.DiscountPct, DiscountCents: c.DiscountCents,
		TaxCents: c.TaxCents, TaxPct: c.TaxPct, DeliveryFeeCents: c.DeliveryFeeCents,
		ServiceChargeCents: c.ServiceChargeCents, TipCents: c.TipCents,
	}
}

type posUpdateOrderRequest struct {
	// Lines with an id are updated, lines without are added. Anything not
	// mentioned is left as it is, so a screen can send only what changed.
	Items []posItemRequest `json:"items,omitempty"`
	// Order status, if the till is moving it.
	Status   string  `json:"status,omitempty"`
	StatusAt *string `json:"statusAt,omitempty" example:"2026-09-16T10:33:52Z"`
	Notes    string  `json:"notes,omitempty"`

	CustomerID            ordersapi.OptionalString `json:"customerId,omitempty" swaggertype:"string" example:"c0a80123-4567-89ab-cdef-0123456789ab"`
	GuestCustomerName     *string                  `json:"guestCustomerName,omitempty" example:"Jane Doe"`
	GuestCustomerMobile   *string                  `json:"guestCustomerMobile,omitempty" example:"+1234567890"`
	GuestCustomerAddress  *string                  `json:"guestCustomerAddress,omitempty" example:"123 Flower Street"`
	GuestCustomerCity     *string                  `json:"guestCustomerCity,omitempty" example:"Edison"`
	GuestCustomerPostcode *string                  `json:"guestCustomerPostcode,omitempty" example:"08817"`
	GuestCustomerEmail    *string                  `json:"guestCustomerEmail,omitempty" example:"guest@example.com"`
	OrderDeliverDate      *string                  `json:"orderDeliverDate,omitempty" example:"2026-09-07"`
	OrderDeliverTime      *string                  `json:"orderDeliverTime,omitempty" example:"14:30:00"`

	// Customer details. Written onto the customer record only when
	// customerId is sent; without a customerId no customer record changes.
	// The order's purchaser is then refreshed from that customer.
	Customer *posCustomerRequest `json:"customer,omitempty"`
	// Who the order is for (storefront-style). Omit to keep it as it is.
	Recipient *posRecipientRequest `json:"recipient,omitempty"`
	// Gift message and occasion. Key omitted keeps the stored value; null or
	// "" clears it; a string replaces it.
	GiftMessage ordersapi.OptionalString `json:"giftMessage,omitempty" swaggertype:"string" example:"Happy birthday, Amma!"`
	GiftReason  ordersapi.OptionalString `json:"giftReason,omitempty" swaggertype:"string" example:"Birthday"`
	// "Send as a gift". Omit to keep.
	IsGift *bool `json:"isGift,omitempty" example:"true"`
	// pickup, delivery or takeaway. Omit to keep it. Moving to delivery adds
	// the outlet's delivery fee (moving off delivery removes it) unless
	// deliveryFeeCents is sent.
	FulfilmentType *string `json:"fulfilmentType,omitempty" enums:"pickup,delivery,takeaway" example:"delivery"`

	// KDS: flag the order "Amended" on the kitchen display (send true when the
	// change matters to a kitchen that already has the ticket).
	MarkAmended bool `json:"markAmended,omitempty"`
	// KDS: when it was amended (RFC3339). Defaults to statusAt, else now.
	AmendedAt *string `json:"amendedAt,omitempty" example:"2026-09-16T10:33:52Z"`
	// KDS: the order's kitchen status, if the frontend is setting it.
	KotStatus string `json:"kotStatus,omitempty" example:"queue"`

	posChargesPatchRequest
}

type posUpdateOrderResponse struct {
	Order   orderDTOType `json:"order"`
	BatchNo int          `json:"batchNo"`
}

// @Summary		Update an order
// @Description	Everything that happens to an order after it is placed: more lines, a changed quantity, a line cancelled, a batch sent to the kitchen, a discount. Lines carrying an id are updated; lines without one are added; lines not mentioned are left alone. Nothing is deleted — a cancelled line keeps its place and stops counting, because the kitchen already has a docket with it on. Charges you do not send keep their stored value. fulfilmentType changes pickup / delivery / takeaway. With customerId, the customer object (or the guest fields) updates that customer's record and the purchaser is refreshed from it; without customerId no customer record is changed. recipient sets who the order is for.
// @Tags			pos
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path	string	true	"orderId"
// @Param			body	body	posUpdateOrderRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope{data=posUpdateOrderResponse}
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/pos/orders/{orderId} [put]
func (h *Handler) UpdatePOSOrder(w http.ResponseWriter, r *http.Request) {
	var req posUpdateOrderRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	// Build extra fields only when at least one guest/delivery field is present.
	var extra *domain.POSOrderExtraFields
	if req.CustomerID.Set || req.GuestCustomerName != nil || req.GuestCustomerMobile != nil ||
		req.GuestCustomerAddress != nil || req.GuestCustomerCity != nil || req.GuestCustomerPostcode != nil ||
		req.GuestCustomerEmail != nil || req.OrderDeliverDate != nil || req.OrderDeliverTime != nil ||
		req.Customer != nil || req.Recipient != nil || req.FulfilmentType != nil ||
		req.GiftMessage.Set || req.GiftReason.Set || req.IsGift != nil {
		extra = &domain.POSOrderExtraFields{
			CustomerID:            req.CustomerID.Value,
			ClearCustomerID:       req.CustomerID.Set && req.CustomerID.Value == nil,
			GuestCustomerName:     req.GuestCustomerName,
			GuestCustomerMobile:   req.GuestCustomerMobile,
			GuestCustomerAddress:  req.GuestCustomerAddress,
			GuestCustomerCity:     req.GuestCustomerCity,
			GuestCustomerPostcode: req.GuestCustomerPostcode,
			GuestCustomerEmail:    req.GuestCustomerEmail,
			OrderDeliverDate:      parsePOSDateStr(req.OrderDeliverDate),
			OrderDeliverTime:      parsePOSTime(req.OrderDeliverTime),
			Customer:              req.Customer.toDomain(),
			Recipient:             req.Recipient.toDomain(),
			FulfilmentType:        req.FulfilmentType,
			GiftMessage:           giftPatch(req.GiftMessage),
			GiftReason:            giftPatch(req.GiftReason),
			IsGift:                req.IsGift,
		}
	}

	r, err := httpx.WithEventTime(r, "statusAt", req.StatusAt)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var kds *domain.POSKDSFields
	if req.MarkAmended || strings.TrimSpace(req.KotStatus) != "" {
		kds = &domain.POSKDSFields{MarkAmended: req.MarkAmended, KOTStatus: req.KotStatus}
		if req.AmendedAt != nil && strings.TrimSpace(*req.AmendedAt) != "" {
			t, ok := timex.ParseEventTime(req.AmendedAt)
			if !ok {
				httpx.WriteError(w, r, apperr.Validation("amendedAt must be an RFC3339 date-time.").
					WithFields(apperr.FieldError{Field: "amendedAt", Code: "INVALID", Message: "Not a date-time."}))
				return
			}
			kds.AmendedAt = t
		}
	}
	order, batchNo, err := h.svc.UpdatePOSOrderKDS(r.Context(), staffClaims(r),
		chi.URLParam(r, "orderId"), toItems(req.Items), req.toPatch(), req.Status, req.Notes, extra, kds)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	dto := orderDTO(order)
	h.enrichCustomerDTO(r.Context(), &dto.Customer, order.CustomerID)
	httpx.OK(w, posUpdateOrderResponse{Order: dto, BatchNo: batchNo})
}

// @Summary		Remove a counter order
// @Description	A soft delete: the order disappears from every list but the row survives for the books.
// @Tags			pos
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path	string	true	"orderId"
// @Success		200	{object}	httpx.Envelope
// @Router			/pos/orders/{orderId} [delete]
func (h *Handler) DeletePOSOrder(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeletePOSOrder(r.Context(), staffClaims(r), chi.URLParam(r, "orderId")); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"deleted": true})
}

type posReturnRequest struct {
	FullReturn bool    `json:"fullReturn,omitempty"`
	Reason     string  `json:"reason,omitempty"`
	ReturnedAt *string `json:"returnedAt,omitempty" example:"2026-09-16T10:33:52Z"`
	Items      []struct {
		OrderItemID string `json:"orderItemId"`
		Quantity    int    `json:"quantity"`
	} `json:"items,omitempty"`

	// Discount on the goods coming back. Cents wins over the percentage;
	// omit both and the order's own discount is applied to what comes back.
	DiscountPct   *float64 `json:"discountPct,omitempty" example:"10"`
	DiscountCents *int64   `json:"discountCents,omitempty" example:"150"`
	// Tax on the goods coming back (after the discount). Cents wins over the
	// percentage; omit both and the order's tax rate is used.
	TaxPct   *float64 `json:"taxPct,omitempty" example:"6.625"`
	TaxCents *int64   `json:"taxCents,omitempty" example:"89"`

	// How the money goes back. Omitted: the return moves no money (as before).
	Refund *returnRefundRequest `json:"refund,omitempty"`
}

// posReturnResponse is the return just recorded.
// refundAmountCents = grossAmountCents - discountCents + taxCents.
type posReturnResponse struct {
	ReturnID string `json:"returnId"`
	// Gross value of the goods (unit price × quantity). Kept as amountCents
	// for existing clients.
	AmountCents      int64 `json:"amountCents"`
	GrossAmountCents int64 `json:"grossAmountCents"`
	returnChargesDTOType
	// Each item on this return with its own discount, tax and refund (item
	// based); the fields above are their totals (return based).
	Items []orderReturnItemDTOType `json:"items"`
	// The refund made, when the request asked for one.
	Refund *returnRefundDTOType `json:"refund,omitempty"`
	Order  orderDTOType         `json:"order"`
}

// @Summary		Return a whole order or some of its lines
// @Description	fullReturn (order based) hands back everything still outstanding; items (return based) hands back particular lines. discountPct / discountCents and taxPct / taxCents are the discount and tax on what comes back — cents wins over the percentage; omitted, the order's own discount and tax rate are applied. Saved on the return and totalled on the order; GET /pos/orders/{orderId} shows both (returns[] and returnSummary).
// @Tags			pos
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path	string	true	"orderId"
// @Param			body	body	posReturnRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope{data=posReturnResponse}
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/pos/orders/{orderId}/return [post]
func (h *Handler) ReturnPOSOrder(w http.ResponseWriter, r *http.Request) {
	var req posReturnRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	lines := make([]ordersapi.ReturnLine, 0, len(req.Items))
	for _, it := range req.Items {
		lines = append(lines, ordersapi.ReturnLine{OrderItemID: it.OrderItemID, Quantity: it.Quantity})
	}

	r, err := httpx.WithEventTime(r, "returnedAt", req.ReturnedAt)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	orderID := chi.URLParam(r, "orderId")
	var refundReq *ordersapi.ReturnRefund
	if req.Refund != nil {
		refundReq = &ordersapi.ReturnRefund{
			PaymentMode: req.Refund.PaymentMode, CardType: req.Refund.CardType,
			RefundType: req.Refund.RefundType, AmountCents: req.Refund.AmountCents,
		}
	}
	returnID, charges, refund, err := h.svc.ReturnPOSOrder(r.Context(), staffClaims(r), orderID,
		ordersapi.ReturnRequest{
			FullReturn: req.FullReturn, Reason: req.Reason, Items: lines,
			DiscountPct: req.DiscountPct, DiscountCents: req.DiscountCents,
			TaxPct: req.TaxPct, TaxCents: req.TaxCents, Refund: refundReq,
		})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out := posReturnResponse{
		ReturnID: returnID, AmountCents: charges.GrossCents, GrossAmountCents: charges.GrossCents,
		returnChargesDTOType: returnChargesDTO(charges),
		Items:                []orderReturnItemDTOType{},
		Refund:               returnRefundDTO(refund),
	}
	if order, err := h.svc.GetOrderAdmin(r.Context(), staffClaims(r), orderID); err == nil {
		out.Order = orderDTO(order)
		for _, ret := range out.Order.Returns {
			if ret.ID == returnID {
				out.Items = ret.Items
			}
		}
		h.enrichCustomerDTO(r.Context(), &out.Order.Customer, order.CustomerID)
	}
	httpx.OK(w, out)
}

type posSplitRequest struct {
	Mode    string   `json:"mode"`
	Ways    int      `json:"ways,omitempty"`
	Amounts []int64  `json:"amounts,omitempty"`
	Labels  []string `json:"labels,omitempty"`
	// The service charge chosen on the bill screen. Set on the order before
	// it is split, so every share carries its part. Omitted: unchanged.
	ServiceChargeCents *int64 `json:"serviceChargeCents,omitempty" example:"500"`
	Groups             [][]struct {
		OrderItemID string `json:"orderItemId"`
		Quantity    int    `json:"quantity"`
	} `json:"groups,omitempty"`
}

// @Summary		Clear a bill split
// @Description	Removes every share of the split and puts the order back on one bill. Refused once part of the split has been paid.
// @Tags			pos
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path		string	true	"orderId"
// @Success		200		{object}	httpx.Envelope
// @Failure		403		{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404		{object}	httpx.Envelope	"NOT_FOUND"
// @Failure		422		{object}	httpx.Envelope	"SPLIT_REFUSED"
// @Router			/pos/orders/{orderId}/split [delete]
func (h *Handler) ClearSplit(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "orderId")
	removed, err := h.svc.ClearSplit(r.Context(), staffClaims(r), orderID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]any{"orderId": orderID, "cleared": true, "billsRemoved": removed})
}

// @Summary		Split a bill by amount, by item, or custom
// @Description	Replaces any previous split. Refused once part of the bill has been paid.
// @Tags			pos
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path	string	true	"orderId"
// @Param			body	body	posSplitRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/pos/orders/{orderId}/split [post]
func (h *Handler) SplitOrder(w http.ResponseWriter, r *http.Request) {
	var req posSplitRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	groups := make([][]ordersapi.BillItem, 0, len(req.Groups))
	for _, g := range req.Groups {
		items := make([]ordersapi.BillItem, 0, len(g))
		for _, it := range g {
			items = append(items, ordersapi.BillItem{OrderItemID: it.OrderItemID, Quantity: it.Quantity})
		}
		groups = append(groups, items)
	}

	bills, err := h.svc.SplitOrder(r.Context(), staffClaims(r), chi.URLParam(r, "orderId"),
		domain.SplitRequest{Mode: req.Mode, Ways: req.Ways, Amounts: req.Amounts, Labels: req.Labels, Groups: groups,
			ServiceChargeCents: req.ServiceChargeCents})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, billsDTO(bills, nil))
}

// @Summary		Read an order's bills and what each has taken
// @Tags			pos
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path	string	true	"orderId"
// @Success		200	{object}	httpx.Envelope
// @Router			/pos/orders/{orderId}/bills [get]
func (h *Handler) ListBills(w http.ResponseWriter, r *http.Request) {
	bills, payments, err := h.svc.ListBills(r.Context(), staffClaims(r), chi.URLParam(r, "orderId"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, billsDTO(bills, payments))
}

type posPaymentRequest struct {
	// Omit for a payment against the whole order rather than one share.
	BillID             *string `json:"billId,omitempty"`
	Method             string  `json:"method"`
	AmountCents        int64   `json:"amountCents"`
	TipCents           int64   `json:"tipCents,omitempty"`
	TenderedCents      *int64  `json:"tenderedCents,omitempty"`
	Reference          string  `json:"reference,omitempty"`
	ServiceChargeCents *int64  `json:"serviceChargeCents,omitempty"`
	PaidAt             *string `json:"paidAt,omitempty" example:"2026-09-16T10:33:52Z"`
}

// @Summary		Take a payment against a bill or the whole order
// @Description	Cash or card. Only cash reaches the drawer and counts towards the day close. When everything owed has been taken the order is marked paid.
// @Tags			pos
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path	string	true	"orderId"
// @Param			body	body	posPaymentRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/pos/orders/{orderId}/payments [post]
func (h *Handler) TakePayment(w http.ResponseWriter, r *http.Request) {
	var req posPaymentRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	billID := req.BillID
	if billID != nil && strings.TrimSpace(*billID) == "" {
		billID = nil
	}
	r, err := httpx.WithEventTime(r, "paidAt", req.PaidAt)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	payment, orderSettled, err := h.svc.TakePayment(r.Context(), staffClaims(r), chi.URLParam(r, "orderId"),
		ordersapi.BillPayment{
			BillID: billID, Method: req.Method, AmountCents: req.AmountCents,
			TipCents: req.TipCents, TenderedCents: req.TenderedCents, Reference: req.Reference,
		}, req.ServiceChargeCents)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	resp := map[string]any{
		"method":      payment.Method,
		"amountCents": payment.AmountCents,
		"tipCents":    payment.TipCents,
		// What to hand back, for cash tendered above what was owed.
		"changeCents":  payment.ChangeCents,
		"orderSettled": orderSettled,
	}
	if req.ServiceChargeCents != nil {
		resp["serviceChargeCents"] = *req.ServiceChargeCents
	}
	// The order as it now stands — status (fulfilled once settled), payment
	// method/type, tip and totals — so the till does not need a second read.
	if order, err := h.svc.GetOrderAdmin(r.Context(), staffClaims(r), chi.URLParam(r, "orderId")); err == nil {
		resp["orderStatus"] = order.Status
		resp["paymentStatus"] = order.PaymentStatus
		resp["paymentType"] = order.PaymentMethod
		resp["orderTotalCents"] = order.TotalCents
		resp["orderTipCents"] = order.TipCents
	}
	httpx.OK(w, resp)
}

func billsDTO(bills []ordersapi.Bill, payments []ordersapi.BillPayment) map[string]any {
	out := make([]map[string]any, 0, len(bills))
	for _, b := range bills {
		items := make([]map[string]any, 0, len(b.Items))
		for _, it := range b.Items {
			items = append(items, map[string]any{
				"orderItemId": it.OrderItemID,
				"productName": it.ProductName,
				"quantity":    it.Quantity,
				"amountCents": it.AmountCents,
			})
		}
		out = append(out, map[string]any{
			"id":          b.ID,
			"splitMode":   b.SplitMode,
			"label":       b.Label,
			"sequenceNo":  b.SequenceNo,
			"amountCents": b.AmountCents,
			"paidCents":   b.PaidCents,
			// What is still owed on this share, so the till does not subtract.
			"outstandingCents": maxInt64(b.AmountCents-b.PaidCents, 0),
			"status":           b.Status,
			"items":            items,
		})
	}

	paid := make([]map[string]any, 0, len(payments))
	for _, p := range payments {
		paid = append(paid, map[string]any{
			"id":            p.ID,
			"billId":        p.BillID,
			"method":        p.Method,
			"amountCents":   p.AmountCents,
			"tipCents":      p.TipCents,
			"tenderedCents": p.TenderedCents,
			"changeCents":   p.ChangeCents,
			"reference":     p.Reference,
			"takenBy":       p.TakenBy,
			"createdAt":     p.CreatedAt,
			// card: stripe (payment link) | manual (own terminal)
			"provider":      p.Provider,
			"providerRef":   p.ProviderRef,
			"paymentLinkId": p.PaymentLinkID,
		})
	}
	return map[string]any{"bills": out, "payments": paid}
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

/*
 * The till's order reads.
 *
 * Delegating to the admin handlers, which are already branch-scoped by the
 * staff token: an order is an order, and a second listing that could drift
 * from the console's is a bug waiting to be found by whoever is holding a
 * receipt. They exist as named functions so /docs shows them under the till's
 * own prefix — a POS client should not have to discover its order list on an
 * /admin path.
 */

// @Summary		List the till's orders
// @Description	The branch's orders, newest first. Same filters as the console: status, search, dateFrom, dateTo, page, limit.
// @Tags			pos
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	query		string	false	"Branch (defaults to the token's scope)"
// @Param			status		query		string	false	"Filter by status"
// @Param			fulfilmentType	query	string	false	"Filter by fulfilment type (pickup, delivery, takeaway)"
// @Param			payment_type	query	string	false	"Filter by payment type (cash, card, all)"
// @Param			channel		query		string	false	"Filter by channel (pos, storefront, manual, all)"
// @Param			orderFilterType	query	string	false	"Preset tab, ANDed with the other filters: active, takeaway, delivery, pickup, online, completed"
// @Param			search		query		string	false	"Free-text search"
// @Param			dateFrom	query		string	false	"YYYY-MM-DD"
// @Param			dateTo		query		string	false	"YYYY-MM-DD"
// @Param			startDate	query		string	false	"YYYY-MM-DD (alias of dateFrom)"
// @Param			endDate		query		string	false	"YYYY-MM-DD (alias of dateTo)"
// @Param			kotStatus	query		string	false	"KDS kitchen status, comma-separated: queue, preparing, ready, packing, ready_for_delivery, out_for_delivery, delivered, completed, cancelled"
// @Param			page		query		int		false	"Page number (default 1)"
// @Param			limit		query		int		false	"Page size (default 50, max 200)"
// @Success		200			{object}	httpx.Envelope{data=[]orderSummaryDTOType}
// @Failure		401			{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403			{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/pos/orders [get]
func (h *Handler) ListPOSOrders(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))

	fulfilmentType := q.Get("fulfilmentType")
	if fulfilmentType == "" {
		fulfilmentType = q.Get("fulfilment_type")
	}

	paymentType := q.Get("payment_type")
	if paymentType == "" {
		paymentType = q.Get("paymentType")
	}
	if paymentType == "" {
		paymentType = q.Get("payment_method")
	}
	if paymentType == "" {
		paymentType = q.Get("paymentMethod")
	}

	channel := q.Get("channel")

	orderFilterType := strings.ToLower(strings.TrimSpace(q.Get("orderFilterType")))
	if orderFilterType == "" {
		orderFilterType = strings.ToLower(strings.TrimSpace(q.Get("order_filter_type")))
	}
	if orderFilterType != "" && orderFilterType != "all" {
		if _, _, _, _, ok := ordersapi.OrderFilterTypeConditions(orderFilterType); !ok {
			httpx.WriteError(w, r, apperr.Validation("orderFilterType must be one of: "+strings.Join(ordersapi.AllOrderFilterTypes(), ", ")+"."))
			return
		}
	} else {
		orderFilterType = ""
	}
	status := q.Get("status")
	for _, part := range strings.Split(status, ",") {
		v := ordersapi.NormalizeStatus(part)
		if v != "" && v != "all" && !ordersapi.IsValidStatus(v) && v != ordersapi.StatusReturned && v != ordersapi.StatusRefunded {
			httpx.WriteError(w, r, apperr.Validation("status must be one of: "+strings.Join(append(ordersapi.AllStatuses(), ordersapi.StatusReturned, ordersapi.StatusRefunded), ", ")+"."))
			return
		}
	}
	for _, part := range strings.Split(fulfilmentType, ",") {
		v := strings.ToLower(strings.TrimSpace(part))
		switch v {
		case "", "all", ordersapi.FulfilmentPickup, ordersapi.FulfilmentDelivery, ordersapi.FulfilmentTakeaway:
		default:
			httpx.WriteError(w, r, apperr.Validation("fulfilmentType must be one of: pickup, delivery, takeaway."))
			return
		}
	}

	f := ordersapi.ListOrdersFilter{
		BranchID:        q.Get("branchId"),
		Status:          status,
		FulfilmentType:  fulfilmentType,
		PaymentType:     paymentType,
		Channel:         channel,
		OrderFilterType: orderFilterType,
		Search:          q.Get("search"),
		Sort:            q.Get("sort"),
		Page:            page,
		Limit:           limit,
	}
	if v := q.Get("dateFrom"); v != "" {
		if t, err := timex.ParseDate(v); err == nil {
			f.DateFrom = &t
		}
	}
	if v := q.Get("dateTo"); v != "" {
		if t, err := timex.ParseDate(v); err == nil {
			f.DateTo = &t
		}
	}
	if err := applyKDSListFilters(q, &f); err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	orders, total, err := h.svc.ListOrdersAdmin(r.Context(), staffClaims(r), f)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	// Totals for everything the filter matched, not just this page.
	stats := mapOrderStats(orders, int64(total))
	if sum, err := h.svc.SummarizeOrdersAdmin(r.Context(), staffClaims(r), f); err == nil {
		applyListSummary(&stats, sum)
	}
	out := make([]orderSummaryDTOType, 0, len(orders))
	for _, o := range orders {
		out = append(out, orderSummaryDTO(&o))
	}

	p := page
	if p < 1 {
		p = 1
	}
	l := limit
	if l < 1 || l > 100 {
		l = 25
	}
	totalPages := 0
	if l > 0 {
		totalPages = (total + l - 1) / l
	}
	httpx.OKWithMetaAndStats(w, out, httpx.Meta{
		Page:       p,
		PerPage:    l,
		Total:      total,
		TotalPages: totalPages,
	}, stats)
}

// @Summary		Get one order at the till
// @Description	The full order — lines with their kitchen batch, totals, payments and returns.
// @Tags			pos
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path		string	true	"orderId"
// @Success		200		{object}	httpx.Envelope{data=orderDTOType}
// @Failure		403		{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404		{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/pos/orders/{orderId} [get]
func (h *Handler) GetPOSOrder(w http.ResponseWriter, r *http.Request) {
	h.GetOrderAdmin(w, r)
}

/*
 * Moving an order along from the till.
 *
 * These delegate to the admin handlers for the same reason the reads do: an
 * order's status means one thing, and a second path that could move it
 * differently is how a website order ends up "delivered" in one screen and
 * "ready" in another.
 *
 * They matter most for orders the till did not take. A website order arrives
 * already paid and has to be walked to fulfilled by whoever hands it over —
 * and until these existed the counter had no way to do that, because the only
 * status endpoints lived under /admin.
 */

// @Summary		Move an order to another status
// @Description	Any status in the workflow: placed, confirmed, scheduled, in_production, ready, out_for_delivery, fulfilled, cancelled. Use this to walk a website order to delivered from the counter.
// @Tags			pos
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path		string				true	"orderId"
// @Param			body	body		setStatusRequest	true	"Request body"
// @Success		200		{object}	httpx.Envelope
// @Failure		400		{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		403		{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404		{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/pos/orders/{orderId}/status [post]
func (h *Handler) SetPOSOrderStatus(w http.ResponseWriter, r *http.Request) {
	h.SetOrderStatusAdmin(w, r)
}

// @Summary		Mark an order delivered or collected
// @Description	Closes the order out and emails the customer. The one-tap version of setting status to fulfilled — which is what a counter does when it hands a website order over.
// @Tags			pos
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path		string	true	"orderId"
// @Success		200		{object}	httpx.Envelope
// @Failure		403		{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404		{object}	httpx.Envelope	"NOT_FOUND"
// @Param			body	body	markStatusRequest	false	"Request body (statusAt optional)"
// @Router			/pos/orders/{orderId}/mark-fulfilled [post]
func (h *Handler) MarkPOSOrderFulfilled(w http.ResponseWriter, r *http.Request) {
	h.MarkFulfilledAdmin(w, r)
}

// @Summary		Cancel an order
// @Description	The order did not happen. Distinct from removing a counter order, which says it never should have existed, and from a return, which says the goods came back. A reason is required.
// @Tags			pos
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path		string	true	"orderId"
// @Success		200		{object}	httpx.Envelope
// @Failure		400		{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		403		{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404		{object}	httpx.Envelope	"NOT_FOUND"
// @Param			body	body	cancelOrderRequest	true	"Request body (statusAt optional)"
// @Router			/pos/orders/{orderId}/cancel [post]
func (h *Handler) CancelPOSOrder(w http.ResponseWriter, r *http.Request) {
	h.CancelOrderAdmin(w, r)
}

// @Summary		Refund an order from the till
// @Description	Gives money back. A return records goods coming back and adjusts the order's totals; it moves no money, so a customer owed a refund needs this as well.
// @Tags			pos
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			orderId	path		string	true	"orderId"
// @Success		200		{object}	httpx.Envelope
// @Failure		400		{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		403		{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404		{object}	httpx.Envelope	"NOT_FOUND"
// @Param			body	body	refundRequest	true	"Request body (statusAt optional)"
// @Router			/pos/orders/{orderId}/refund [post]
func (h *Handler) RefundPOSOrder(w http.ResponseWriter, r *http.Request) {
	h.RefundOrderAdmin(w, r)
}

func parsePOSDate(raw *string) *time.Time {
	if raw == nil {
		return nil
	}
	s := strings.TrimSpace(*raw)
	if s == "" {
		return nil
	}
	layouts := []string{
		"2006-01-02",
		"02 January 2006",
		"2 January 2006",
		"02 Jan 2006",
		"2 Jan 2006",
		time.RFC3339,
		"2006-01-02T15:04:05.000Z07:00",
		"2006-01-02T15:04:05Z07:00",
		"02/01/2006",
		"2006/01/02",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
			return &d
		}
	}
	return nil
}

func parsePOSDateStr(raw *string) *string {
	if raw == nil {
		return nil
	}
	if t := parsePOSDate(raw); t != nil {
		s := t.Format("2006-01-02")
		return &s
	}
	s := strings.TrimSpace(*raw)
	if s == "" {
		return nil
	}
	return &s
}

func parsePOSTime(raw *string) *string {
	if raw == nil {
		return nil
	}
	s := strings.TrimSpace(*raw)
	if s == "" {
		return nil
	}
	layouts := []string{
		"15:04:05",
		"15:04",
		"3:04 PM",
		"3:04PM",
		"03:04 PM",
		"03:04PM",
		"3:04 pm",
		"3:04pm",
		"03:04 pm",
		"03:04pm",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			out := t.Format("15:04:05")
			return &out
		}
	}
	return &s
}

type posDeliveryQuoteRequest struct {
	BranchID string `json:"branchId" example:"0c993b06-8dd2-4eb4-8380-8eb188a2f39f"`
	// The delivery address. Street is optional for a ZIP-priced store and
	// improves accuracy for a radius-priced one.
	Street   string `json:"street,omitempty" example:"53 Wilk Rd"`
	City     string `json:"city,omitempty" example:"Edison"`
	State    string `json:"state,omitempty" example:"NJ"`
	Postcode string `json:"postcode" example:"08837"`
	// Item subtotal before discounts, tip, tax and delivery: what "free over
	// $X" rules compare against.
	SubtotalCents int64 `json:"subtotalCents" example:"6800"`
	// The delivery slot's start ("HH:MM"), for charge-by-delivery-time rules.
	SlotStart string `json:"slotStart,omitempty" example:"14:00"`
}

// @Summary		Quote a delivery fee at the till
// @Description	The store's delivery pricing for an address, order size and slot. An address outside the store's area comes back with deliverable=false and a reason; the till may still take the order with a fee staff type in. Sending deliveryFeeCents 0 (or omitting it) on POST /pos/orders applies this quote automatically.
// @Tags			pos
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body		posDeliveryQuoteRequest	true	"Address, subtotal and slot"
// @Success		200		{object}	httpx.Envelope{data=franchiseapi.DeliveryQuote}
// @Failure		400		{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		403		{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404		{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/pos/delivery/quote [post]
func (h *Handler) POSDeliveryQuote(w http.ResponseWriter, r *http.Request) {
	var req posDeliveryQuoteRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	// Named here so swag can resolve the @Success type through this file's imports.
	var q *franchiseapi.DeliveryQuote
	q, err := h.svc.QuoteDeliveryPOS(r.Context(), staffClaims(r), domain.DeliveryQuoteRequest{
		BranchID: req.BranchID, Street: req.Street, City: req.City, State: req.State, Postcode: req.Postcode,
		SubtotalCents: req.SubtotalCents, SlotStart: req.SlotStart,
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, q)
}
