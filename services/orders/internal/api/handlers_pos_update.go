package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/httpx"
	"bakehouse/pkg/timex"
	"bakehouse/services/orders/internal/domain"
	"bakehouse/services/orders/ordersapi"
)

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
