package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/httpx"
	"bakehouse/pkg/timex"
	"bakehouse/services/orders/internal/domain"
)

type setFulfilmentRequest struct {
	BranchID       *string `json:"branchId,omitempty"`
	FulfilmentType string  `json:"fulfilmentType"`
	Address        *string `json:"address,omitempty"`
	Street         *string `json:"street,omitempty"`
	City           *string `json:"city,omitempty"`
	State          *string `json:"state,omitempty"`
	Postcode       *string `json:"postcode,omitempty"`
}

// @Summary		Set pickup or delivery and the fulfilling branch
// @Tags			storefront
// @Accept			json
// @Produce		json
// @Param			X-Cart-Token	header	string	false	"Guest cart token; omit when authenticated"
// @Param			body	body	setFulfilmentRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/storefront/cart/fulfilment [post]
func (h *Handler) SetFulfilment(w http.ResponseWriter, r *http.Request) {
	var req setFulfilmentRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	street := req.Street
	if street == nil {
		street = req.Address
	}
	cart, summary, err := h.svc.SetFulfilment(r.Context(), h.cartIdentity(r), req.FulfilmentType,
		street, req.City, req.State, req.Postcode, req.BranchID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, cartDTO(cart, summary))
}

type setScheduleRequest struct {
	Date      string `json:"date"`
	SlotStart string `json:"slotStart"`
}

// @Summary		Choose the delivery or pickup slot
// @Tags			storefront
// @Accept			json
// @Produce		json
// @Param			X-Cart-Token	header	string	false	"Guest cart token; omit when authenticated"
// @Param			body	body	setScheduleRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/storefront/cart/schedule [post]
func (h *Handler) SetSchedule(w http.ResponseWriter, r *http.Request) {
	var req setScheduleRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	date, err := timex.ParseDate(req.Date)
	if err != nil {
		httpx.WriteError(w, r, apperr.Validation("date must be YYYY-MM-DD."))
		return
	}
	cart, summary, err := h.svc.SetSchedule(r.Context(), h.cartIdentity(r), date, req.SlotStart)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, cartDTO(cart, summary))
}

type setTipRequest struct {
	FixedCents int64 `json:"fixedCents"`
}

// @Summary		Set the tip amount
// @Tags			storefront
// @Accept			json
// @Produce		json
// @Param			X-Cart-Token	header	string	false	"Guest cart token; omit when authenticated"
// @Param			body	body	setTipRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/storefront/cart/tip [post]
func (h *Handler) SetTip(w http.ResponseWriter, r *http.Request) {
	var req setTipRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	summary, err := h.svc.SetTip(r.Context(), h.cartIdentity(r), req.FixedCents)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, priceSummaryDTO(summary))
}

type applyCouponRequest struct {
	Code string `json:"code"`
}

// @Summary		Apply a discount code to the cart
// @Tags			storefront
// @Accept			json
// @Produce		json
// @Param			X-Cart-Token	header	string	false	"Guest cart token; omit when authenticated"
// @Param			body	body	applyCouponRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"COUPON_INVALID"
// @Router			/storefront/cart/coupon [post]
func (h *Handler) ApplyCoupon(w http.ResponseWriter, r *http.Request) {
	var req applyCouponRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	cart, summary, err := h.svc.ApplyCoupon(r.Context(), h.cartIdentity(r), req.Code)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, cartDTO(cart, summary))
}

// @Summary		Remove the cart's discount code
// @Tags			storefront
// @Produce		json
// @Param			X-Cart-Token	header	string	false	"Guest cart token; omit when authenticated"
// @Success		200	{object}	httpx.Envelope
// @Router			/storefront/cart/coupon [delete]
func (h *Handler) RemoveCoupon(w http.ResponseWriter, r *http.Request) {
	cart, summary, err := h.svc.RemoveCoupon(r.Context(), h.cartIdentity(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, cartDTO(cart, summary))
}

// --- Checkout --------------------------------------------------------------------

// validateCartRequest is optional: what the storefront is showing, so a
// changed delivery charge or total is flagged before payment.
type validateCartRequest struct {
	DeliveryFeeCents *int64 `json:"deliveryFeeCents,omitempty" example:"500"`
	TotalCents       *int64 `json:"totalCents,omitempty" example:"7399"`
}

// @Summary		Re-validate the cart before payment
// @Description	Re-checks prices, availability, the slot and the delivery charge (priced now from the store's rules for the address, subtotal and slot). Optional body: the deliveryFeeCents / totalCents the storefront is showing; a difference fails validation with DELIVERY_FEE_CHANGED / TOTAL_CHANGED. Out of area fails with DELIVERY_UNAVAILABLE. The response carries the current summary.
// @Tags			storefront
// @Accept			json
// @Produce		json
// @Param			X-Cart-Token	header	string	false	"Guest cart token; omit when authenticated"
// @Param			body	body	validateCartRequest	false	"What the storefront is showing (optional)"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/storefront/checkout/validate [post]
func (h *Handler) ValidateCart(w http.ResponseWriter, r *http.Request) {
	var req validateCartRequest
	if r.ContentLength != 0 {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
		if len(bytes.TrimSpace(body)) > 0 {
			dec := json.NewDecoder(bytes.NewReader(body))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&req); err != nil {
				httpx.WriteError(w, r, apperr.Validation("Request body is not valid JSON: "+err.Error()))
				return
			}
		}
	}
	result, err := h.svc.ValidateCart(r.Context(), h.cartIdentity(r), &domain.ValidationExpectation{
		DeliveryFeeCents: req.DeliveryFeeCents, TotalCents: req.TotalCents,
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	warnings := make([]map[string]string, 0, len(result.Warnings))
	for _, wr := range result.Warnings {
		warnings = append(warnings, map[string]string{"type": wr.Type, "message": wr.Message})
	}
	httpx.OK(w, map[string]interface{}{"valid": result.Valid, "warnings": warnings, "summary": priceSummaryDTO(result.Summary)})
}

type createOrderRequest struct {
	Recipient struct {
		FirstName string  `json:"firstName"`
		LastName  string  `json:"lastName"`
		Phone     *string `json:"phone"`
		// Email is the receiver's own, not the purchaser's — the receipt goes
		// to whoever paid. Optional; most gift orders never have it.
		Email *string `json:"email"`
	} `json:"recipient"`
	GiftMessage *string `json:"giftMessage"`
	// GiftReason — "Birthday", "Sympathy", and so on. Free text: the list of
	// occasions belongs to the storefront, not to this contract.
	GiftReason *string `json:"giftReason"`
	IsGift     bool    `json:"isGift"`
	Purchaser  struct {
		FirstName string  `json:"firstName"`
		LastName  string  `json:"lastName"`
		Email     string  `json:"email"`
		Phone     *string `json:"phone"`
	} `json:"purchaser"`
	DeliveryInstructions *string `json:"deliveryInstructions"`
	// Notes is the customer's free-text note on the order as a whole (shown
	// on the KDS), distinct from the courier-facing delivery instructions.
	Notes *string `json:"notes"`
	// BusinessName is the company on the door when delivering to a workplace.
	// DeliveryAddressType is "residence" or "business". Both optional and
	// ignored for pickup.
	BusinessName        *string `json:"businessName" example:"Acme Ltd"`
	DeliveryAddressType *string `json:"deliveryAddressType" enums:"residence,business" example:"business"`
	// Delivery preferences, as flags rather than prose in the instructions,
	// so despatch can filter and act on them. Ignored for pickup.
	PhotoOnDelivery  bool `json:"photoOnDelivery"`
	SurpriseDelivery bool `json:"surpriseDelivery"`
	MarketingOptIn   bool `json:"marketingOptIn"`
	Payment          struct {
		// Method is "card" (the default when omitted, and what every order
		// before cash on delivery was) or "cash_on_delivery". COD skips the
		// processor entirely and places the order unpaid — see
		// domain.CreateOrder.
		Method string `json:"method"`
		// SourceID is a Stripe PaymentMethod id (pm_…) for a client that
		// collects the card itself. Card details never reach this backend.
		PaymentMethodID string `json:"sourceId"`
		// PaymentID is sent instead when the customer paid on the hosted
		// Stripe Checkout page: the PaymentIntent id read back from
		// GET /storefront/payments/checkout-session/{id}. The server
		// re-verifies it against Stripe rather than creating a new payment.
		PaymentIntentID string `json:"paymentId"`
	} `json:"payment"`
	Status string `json:"status,omitempty" example:"confirmed"`
	// PlacedAt / StartTime are the moment the order was placed on the
	// client's clock, stored as start_time (and as the order's created_at
	// bucket for reports); startTime is the same thing under the POS name,
	// used when placedAt is absent. EndTime is stored as end_time when the
	// order is placed straight into a closing status (fulfilled, cancelled).
	// All optional RFC3339 date-times; omitted means the server clock.
	PlacedAt  *string `json:"placedAt,omitempty" example:"2026-09-16T10:33:52Z"`
	StartTime *string `json:"startTime,omitempty" example:"2026-09-16T10:33:52Z"`
	EndTime   *string `json:"endTime,omitempty" example:"2026-09-16T10:45:10Z"`
	// SendKotTime is when the order was sent to the kitchen (KOT), on the
	// client's clock. Stamped on every line of the order. Optional; omitted
	// or null leaves the lines unsent.
	SendKotTime *timex.FlexTime `json:"sendKotTime,omitempty" swaggertype:"string" example:"2026-09-16T10:35:00Z"`
}

// @Summary		Place the order
// @Description	Places the order from the current cart. Optional `status` selects the status the order lands in (placed, confirmed, scheduled, in_production, ready, out_for_delivery, fulfilled); omitted = placed then auto-accepted as confirmed. Optional `startTime` (alias `placedAt`) and `endTime` are RFC3339 date-times from the client's clock, stored as the order's start_time / end_time; end_time is kept only when the order lands in a closing status.
// @Tags			storefront
// @Accept			json
// @Produce		json
// @Param			X-Cart-Token	header	string	false	"Guest cart token; omit when authenticated"
// @Param			Idempotency-Key	header	string	true	"Client-generated UUID; replays return the original response"
// @Param			body	body	createOrderRequest	true	"Request body (status: optional order status, e.g. confirmed / scheduled)"
// @Success		201	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/storefront/orders [post]
func (h *Handler) CreateOrder(w http.ResponseWriter, r *http.Request) {
	idempotencyKey := r.Header.Get("Idempotency-Key")
	var req createOrderRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	placedAt, placedField := req.PlacedAt, "placedAt"
	if placedAt == nil && req.StartTime != nil {
		placedAt, placedField = req.StartTime, "startTime"
	}
	r, err := httpx.WithEventTime(r, placedField, placedAt)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	endTime, ok := timex.ParseEventTime(req.EndTime)
	if !ok {
		httpx.WriteError(w, r, apperr.Validation("endTime must be an RFC3339 date-time, e.g. 2026-09-16T10:45:10Z.").
			WithFields(apperr.FieldError{Field: "endTime", Code: "INVALID", Message: "Not a date-time."}))
		return
	}
	order, err := h.svc.CreateOrder(r.Context(), h.cartIdentity(r), domain.CreateOrderRequest{
		Recipient: domain.Recipient{
			FirstName: req.Recipient.FirstName, LastName: req.Recipient.LastName,
			Phone: req.Recipient.Phone, Email: req.Recipient.Email,
		},
		GiftMessage: req.GiftMessage, GiftReason: req.GiftReason, IsGift: req.IsGift,
		Purchaser:            domain.Purchaser{FirstName: req.Purchaser.FirstName, LastName: req.Purchaser.LastName, Email: req.Purchaser.Email, Phone: req.Purchaser.Phone},
		DeliveryInstructions: req.DeliveryInstructions, MarketingOptIn: req.MarketingOptIn,
		Notes:        req.Notes,
		BusinessName: req.BusinessName, DeliveryAddressType: req.DeliveryAddressType,
		PhotoOnDelivery: req.PhotoOnDelivery, SurpriseDelivery: req.SurpriseDelivery,
		PaymentMethod:   req.Payment.Method,
		PaymentMethodID: req.Payment.PaymentMethodID, PaymentIntentID: req.Payment.PaymentIntentID,
		IdempotencyKey: idempotencyKey,
		Status:         req.Status,
		EndTime:        endTime,
		SendKotTime:    req.SendKotTime.Ptr(),
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Created(w, orderDTO(order))
}
