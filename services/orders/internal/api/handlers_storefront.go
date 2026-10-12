package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/httpx"
	"bakehouse/pkg/timex"
	"bakehouse/services/orders/internal/domain"
	"bakehouse/services/orders/ordersapi"
)

type Handler struct {
	svc *domain.Service
}

func NewHandler(svc *domain.Service) *Handler {
	return &Handler{svc: svc}
}

// cartIdentity resolves the caller's cart identity from either a customer
// bearer token (optional — guest checkout is always allowed, SF-29) or the
// X-Cart-Token header the client stores after its first cart action.
func (h *Handler) cartIdentity(r *http.Request) domain.CartIdentity {
	if claims, ok := auth.CustomerFromContext(r.Context()); ok {
		// The guest token too, when a just-signed-in shopper still sends it:
		// their guest cart is merged into the account's.
		return domain.CartIdentity{CustomerID: &claims.CustomerID, GuestToken: r.Header.Get("X-Cart-Token")}
	}
	return domain.CartIdentity{GuestToken: r.Header.Get("X-Cart-Token")}
}

func writeCartToken(w http.ResponseWriter, token string) {
	if token != "" {
		w.Header().Set("X-Cart-Token", token)
	}
}

// --- Slots (SF-20, GET /storefront/slots) -------------------------------------

// @Summary		List bookable delivery or pickup slots
// @Description	Slots are cut every 30 minutes from the store's pickup or delivery hours. With productId (repeatable) or cart=true, slots inside a seasonal tag's restricted dates for those products are left out; unavailableDates lists the days that lost every slot to a restriction, restrictions lists the windows.
// @Param			branchId		query	string	true	"Store"
// @Param			fulfilmentType	query	string	true	"pickup | delivery"
// @Param			startDate		query	string	false	"YYYY-MM-DD (default today)"
// @Param			days			query	int		false	"How many days (default 14)"
// @Param			productId		query	string	false	"Products whose restricted dates apply (repeatable)"
// @Param			cart			query	bool	false	"true: the restricted dates of every product in the caller's cart"
// @Tags			storefront
// @Produce		json
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/storefront/slots [get]
func (h *Handler) GetSlots(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	branchID := q.Get("branchId")
	fulfilmentType := q.Get("fulfilmentType")
	if branchID == "" || (fulfilmentType != ordersapi.FulfilmentPickup && fulfilmentType != ordersapi.FulfilmentDelivery) {
		httpx.WriteError(w, r, apperr.Validation("branchId and fulfilmentType (pickup|delivery) are required."))
		return
	}
	start := time.Now()
	if v := q.Get("startDate"); v != "" {
		if t, err := timex.ParseDate(v); err == nil {
			start = t
		}
	}
	days := 14
	if v := q.Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			days = n
		}
	}

	// Restricted dates: for products (the product page) or for every product
	// in the caller's cart ("choose a new date").
	productIDs := multiValueQuery(q, "productId")
	if q.Get("cart") == "true" {
		productIDs = append(productIDs, h.svc.CartProductIDs(r.Context(), h.cartIdentity(r))...)
	}
	daySlots, cutoffNote, info, err := h.svc.GetAvailableSlotsFor(r.Context(), branchID, fulfilmentType, start, days, productIDs)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out := make([]daySlotsDTOType, 0, len(daySlots))
	for _, d := range daySlots {
		out = append(out, daySlotsDTO(&d))
	}
	restrictions := make([]map[string]any, 0, len(info.Restrictions))
	for _, pr := range info.Restrictions {
		restrictions = append(restrictions, map[string]any{
			"productId": pr.ProductID, "tagId": pr.TagID, "tagName": pr.TagName,
			"from": pr.From.Format("2006-01-02T15:04:05"), "to": pr.To.Format("2006-01-02T15:04:05"),
			"repeatYearly": pr.RepeatYearly,
		})
	}
	httpx.OK(w, map[string]interface{}{
		"data": out, "cutoffNote": cutoffNote,
		// Dates that lose every slot to a restricted tag: "not available
		// for this product" on the calendar.
		"unavailableDates": info.UnavailableDates,
		"restrictions":     restrictions,
	})
}

// addressSearchResponse is the typed shape of a successful /address/search
// call — declared purely so Swagger renders the real field, not a bare
// "object", in the generated docs.
type addressSearchResponse struct {
	Results []ordersapi.AddressResult `json:"results"`
}

// @Summary		Search for an address
// @Description	Cache-first address finder: checks the database for this
// @Description	exact search text first; only calls the third-party
// @Description	geocoder, and caches the result, on a miss.
// @Tags			storefront
// @Produce		json
// @Param			q	query		string	true	"Free-text address search, minimum 3 characters"
// @Success		200	{object}	httpx.Envelope{data=addressSearchResponse}
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/storefront/address/search [get]
func (h *Handler) SearchAddress(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	results, err := h.svc.SearchAddress(r.Context(), q)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, addressSearchResponse{Results: results})
}

// @Summary		Search for an address (POS)
// @Tags			pos
// @Produce		json
// @Security		BearerAuth
// @Param			q	query		string	true	"Free-text address search, minimum 3 characters"
// @Success		200	{object}	httpx.Envelope{data=addressSearchResponse}
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/pos/address/search [get]
func (h *Handler) SearchAddressPOS(w http.ResponseWriter, r *http.Request) {
	h.SearchAddress(w, r)
}

// --- Cart ----------------------------------------------------------------------

// @Summary		Get the current cart
// @Tags			storefront
// @Produce		json
// @Param			X-Cart-Token	header	string	false	"Guest cart token; omit when authenticated"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/storefront/cart [get]
func (h *Handler) GetCart(w http.ResponseWriter, r *http.Request) {
	cart, summary, err := h.svc.GetCart(r.Context(), h.cartIdentity(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if cart == nil {
		httpx.OK(w, map[string]interface{}{"items": []cartItemDTOType{}, "summary": priceSummaryDTO(nil)})
		return
	}
	httpx.OK(w, cartDTO(cart, summary))
}

type cartItemInput struct {
	BranchID       string                          `json:"branchId,omitempty"`
	ProductID      string                          `json:"productId"`
	ProductName    string                          `json:"productName,omitempty"`
	VariantID      *string                         `json:"variantId,omitempty"`
	VariantName    string                          `json:"variantName,omitempty"`
	FlavourIDs     []string                        `json:"selectedFlavourIds,omitempty"`
	AddonIDs       []string                        `json:"selectedAddonIds,omitempty"`
	Flavours       []ordersapi.SelectedModifierRef `json:"selectedFlavours,omitempty"`
	Addons         []ordersapi.SelectedModifierRef `json:"selectedAddons,omitempty"`
	PresentationID *string                         `json:"selectedPresentationId,omitempty"`
	WrapColourID   *string                         `json:"selectedWrapColourId,omitempty"`
	Quantity       int                             `json:"quantity"`
	UnitPriceCents int64                           `json:"unitPriceCents,omitempty"`
	LineTotalCents int64                           `json:"lineTotalCents,omitempty"`
	IsGift         bool                            `json:"isGift,omitempty"`
	Recipient      *struct {
		FirstName string `json:"firstName"`
		LastName  string `json:"lastName"`
	} `json:"recipient,omitempty"`
	GiftMessage *string `json:"giftMessage,omitempty"`
	Comments    string  `json:"comments,omitempty"`
}

type addCartItemRequest struct {
	BranchID       string  `json:"branchId"`
	FulfilmentType string  `json:"fulfilmentType,omitempty"`
	Address        *string `json:"address,omitempty"`
	Street         *string `json:"street,omitempty"`
	City           *string `json:"city,omitempty"`
	State          *string `json:"state,omitempty"`
	Postcode       *string `json:"postcode,omitempty"`
	ScheduledDate  *string `json:"scheduledDate,omitempty"`
	Date           *string `json:"date,omitempty"`
	SlotStart      *string `json:"slotStart,omitempty"`
	SlotEnd        *string `json:"slotEnd,omitempty"`

	// Direct single-item fields (for single-item calls / backward compatibility)
	ProductID      string                          `json:"productId,omitempty"`
	ProductName    string                          `json:"productName,omitempty"`
	VariantID      *string                         `json:"variantId,omitempty"`
	VariantName    string                          `json:"variantName,omitempty"`
	FlavourIDs     []string                        `json:"selectedFlavourIds,omitempty"`
	AddonIDs       []string                        `json:"selectedAddonIds,omitempty"`
	Flavours       []ordersapi.SelectedModifierRef `json:"selectedFlavours,omitempty"`
	Addons         []ordersapi.SelectedModifierRef `json:"selectedAddons,omitempty"`
	PresentationID *string                         `json:"selectedPresentationId,omitempty"`
	WrapColourID   *string                         `json:"selectedWrapColourId,omitempty"`
	Quantity       int                             `json:"quantity,omitempty"`
	UnitPriceCents int64                           `json:"unitPriceCents,omitempty"`
	LineTotalCents int64                           `json:"lineTotalCents,omitempty"`
	IsGift         bool                            `json:"isGift,omitempty"`
	Recipient      *struct {
		FirstName string `json:"firstName"`
		LastName  string `json:"lastName"`
	} `json:"recipient,omitempty"`
	GiftMessage *string `json:"giftMessage,omitempty"`
	Comments    string  `json:"comments,omitempty"`

	// Bulk items list
	Items []cartItemInput `json:"items,omitempty"`
}

// @Summary		Add one or more lines to the cart (optionally sets fulfilment)
// @Tags			storefront
// @Accept			json
// @Produce		json
// @Param			X-Cart-Token	header	string	false	"Guest cart token; omit when authenticated"
// @Param			body	body	addCartItemRequest	true	"Request body (supports single item or items array, with optional fulfilmentType)"
// @Success		201	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/storefront/cart/items [post]
func (h *Handler) AddCartItem(w http.ResponseWriter, r *http.Request) {
	var req addCartItemRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	itemsToAdd := req.Items
	if len(itemsToAdd) == 0 && req.ProductID != "" {
		qty := req.Quantity
		if qty <= 0 {
			qty = 1
		}
		itemsToAdd = []cartItemInput{
			{
				BranchID:       req.BranchID,
				ProductID:      req.ProductID,
				ProductName:    req.ProductName,
				VariantID:      req.VariantID,
				VariantName:    req.VariantName,
				FlavourIDs:     req.FlavourIDs,
				AddonIDs:       req.AddonIDs,
				Flavours:       req.Flavours,
				Addons:         req.Addons,
				PresentationID: req.PresentationID,
				WrapColourID:   req.WrapColourID,
				Quantity:       qty,
				UnitPriceCents: req.UnitPriceCents,
				LineTotalCents: req.LineTotalCents,
				IsGift:         req.IsGift,
				Recipient:      req.Recipient,
				GiftMessage:    req.GiftMessage,
				Comments:       req.Comments,
			},
		}
	}

	if len(itemsToAdd) == 0 {
		httpx.WriteError(w, r, apperr.Validation("productId or non-empty items array is required."))
		return
	}

	ident := h.cartIdentity(r)
	var (
		cart       *ordersapi.Cart
		summary    *ordersapi.PriceSummary
		guestToken string
	)

	for _, item := range itemsToAdd {
		branchID := item.BranchID
		if branchID == "" {
			branchID = req.BranchID
		}
		qty := item.Quantity
		if qty <= 0 {
			qty = 1
		}

		flavourIDs := item.FlavourIDs
		if len(flavourIDs) == 0 && len(item.Flavours) > 0 {
			for _, f := range item.Flavours {
				if f.ID != "" {
					flavourIDs = append(flavourIDs, f.ID)
				}
			}
		}

		addonIDs := item.AddonIDs
		if len(addonIDs) == 0 && len(item.Addons) > 0 {
			for _, a := range item.Addons {
				if a.ID != "" {
					addonIDs = append(addonIDs, a.ID)
				}
			}
		}

		areq := domain.AddItemRequest{
			BranchID: branchID, ProductID: item.ProductID, VariantID: item.VariantID,
			FlavourIDs: flavourIDs, AddonIDs: addonIDs,
			PresentationID: item.PresentationID, WrapColourID: item.WrapColourID,
			Quantity: qty, IsGift: item.IsGift, GiftMessage: item.GiftMessage,
		}
		if item.Recipient != nil {
			areq.RecipientFirst, areq.RecipientLast = &item.Recipient.FirstName, &item.Recipient.LastName
		}

		c, s, tok, err := h.svc.AddItem(r.Context(), ident, areq)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		if tok != "" {
			guestToken = tok
			ident.GuestToken = tok
		}
		cart = c
		summary = s
	}

	if req.FulfilmentType != "" {
		street := req.Street
		if street == nil {
			street = req.Address
		}
		var branchIDPtr *string
		if req.BranchID != "" {
			branchIDPtr = &req.BranchID
		}
		c, s, err := h.svc.SetFulfilment(r.Context(), ident, req.FulfilmentType,
			street, req.City, req.State, req.Postcode, branchIDPtr)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		cart = c
		summary = s
	}

	dateStr := req.Date
	if dateStr == nil {
		dateStr = req.ScheduledDate
	}
	if dateStr != nil && req.SlotStart != nil && *dateStr != "" && *req.SlotStart != "" {
		date, err := timex.ParseDate(*dateStr)
		if err == nil {
			c, s, err := h.svc.SetSchedule(r.Context(), ident, date, *req.SlotStart)
			if err == nil {
				cart = c
				summary = s
			}
		}
	}

	writeCartToken(w, guestToken)
	httpx.Created(w, cartDTO(cart, summary))
}

// The updatable half of addCartItemRequest. Branch, product and variant are
// missing on purpose: those are what make a line the line it is, so changing
// one is a different line, not an edit of this one.
//
// Every field is a pointer or a slice so that "not mentioned" is a state the
// handler can see. The decoder rejects unknown fields, so a field missing here
// is not ignored — it is a 400, which is how `isGift` came to be rejected by
// the endpoint that its own create counterpart accepts.
type updateCartItemRequest struct {
	Quantity       int      `json:"quantity"`
	FlavourIDs     []string `json:"selectedFlavourIds"`
	AddonIDs       []string `json:"selectedAddonIds"`
	PresentationID *string  `json:"selectedPresentationId"`
	WrapColourID   *string  `json:"selectedWrapColourId"`
	IsGift         *bool    `json:"isGift"`
	Recipient      *struct {
		FirstName string `json:"firstName"`
		LastName  string `json:"lastName"`
	} `json:"recipient"`
	GiftMessage *string `json:"giftMessage"`
}

// @Summary		Update a cart line
// @Tags			storefront
// @Accept			json
// @Produce		json
// @Param			cartItemId	path	string	true	"cartItemId"
// @Param			X-Cart-Token	header	string	false	"Guest cart token; omit when authenticated"
// @Param			body	body	updateCartItemRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/storefront/cart/items/{cartItemId} [patch]
func (h *Handler) UpdateCartItem(w http.ResponseWriter, r *http.Request) {
	var req updateCartItemRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var first, last *string
	if req.Recipient != nil {
		first, last = &req.Recipient.FirstName, &req.Recipient.LastName
	}
	cart, summary, err := h.svc.UpdateItem(r.Context(), h.cartIdentity(r), chi.URLParam(r, "cartItemId"),
		req.Quantity, req.FlavourIDs, req.AddonIDs, req.PresentationID, req.WrapColourID, req.IsGift, first, last, req.GiftMessage)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, cartDTO(cart, summary))
}

// @Summary		Remove a cart line
// @Tags			storefront
// @Produce		json
// @Param			cartItemId	path	string	true	"cartItemId"
// @Param			X-Cart-Token	header	string	false	"Guest cart token; omit when authenticated"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/storefront/cart/items/{cartItemId} [delete]
func (h *Handler) RemoveCartItem(w http.ResponseWriter, r *http.Request) {
	cart, summary, err := h.svc.RemoveItem(r.Context(), h.cartIdentity(r), chi.URLParam(r, "cartItemId"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, cartDTO(cart, summary))
}

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

// --- Storefront recipient book ---------------------------------------------------

// recipientOwner resolves who the recipient book belongs to for list and
// create: the purchaser's email from the query string (a guest has nothing
// else to identify them by), plus a signed-in customer's id from the bearer
// token so rows saved under the account are visible too. Get, update and
// delete work on the recipient id alone.
func (h *Handler) recipientOwner(r *http.Request) domain.RecipientOwner {
	owner := domain.RecipientOwner{PurchaserEmail: strings.TrimSpace(r.URL.Query().Get("purchaserEmail"))}
	if claims, ok := auth.CustomerFromContext(r.Context()); ok {
		owner.CustomerID = &claims.CustomerID
	}
	return owner
}

type saveRecipientRequest struct {
	Email     string  `json:"email"`
	FirstName string  `json:"firstName"`
	LastName  string  `json:"lastName"`
	Phone     *string `json:"phone"`
	// BusinessName is the company on the door; DeliveryAddressType is
	// "residence" or "business".
	BusinessName         *string `json:"businessName" example:"Acme Ltd"`
	DeliveryAddressType  *string `json:"deliveryAddressType" enums:"residence,business" example:"business"`
	DeliveryInstructions *string `json:"deliveryInstructions"`
	DeliveryStreet       *string `json:"deliveryStreet"`
	DeliveryCity         *string `json:"deliveryCity"`
	DeliveryState        *string `json:"deliveryState"`
	DeliveryPostcode     *string `json:"deliveryPostcode"`
}

func (req saveRecipientRequest) toDomain() domain.SaveRecipientRequest {
	return domain.SaveRecipientRequest{
		Email: req.Email, FirstName: req.FirstName, LastName: req.LastName, Phone: req.Phone,
		BusinessName: req.BusinessName, DeliveryAddressType: req.DeliveryAddressType,
		DeliveryInstructions: req.DeliveryInstructions,
		DeliveryStreet:       req.DeliveryStreet, DeliveryCity: req.DeliveryCity,
		DeliveryState: req.DeliveryState, DeliveryPostcode: req.DeliveryPostcode,
	}
}

// @Summary		List saved storefront recipients
// @Description	Recipients saved for a purchaser, keyed by email. A signed-in customer also sees rows saved under their account.
// @Tags			storefront
// @Produce		json
// @Param			purchaserEmail	query	string	true	"Purchaser email"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router		/storefront/recipients [get]
func (h *Handler) ListStorefrontRecipients(w http.ResponseWriter, r *http.Request) {
	recipients, err := h.svc.ListStorefrontRecipients(r.Context(), h.recipientOwner(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, recipients)
}

// @Summary		Get one saved recipient
// @Tags			storefront
// @Produce		json
// @Param			recipientId	path	string	true	"recipientId"
// @Success		200	{object}	httpx.Envelope
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router		/storefront/recipients/{recipientId} [get]
func (h *Handler) GetStorefrontRecipient(w http.ResponseWriter, r *http.Request) {
	recipient, err := h.svc.GetStorefrontRecipient(r.Context(), chi.URLParam(r, "recipientId"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, recipient)
}

// @Summary		Save a recipient
// @Description	Adds a recipient to the purchaser's book. Posting an email already in the book updates that entry rather than creating a duplicate.
// @Tags			storefront
// @Accept			json
// @Produce		json
// @Param			purchaserEmail	query	string	true	"Purchaser email"
// @Param			body	body	saveRecipientRequest	true	"Request body"
// @Success		201	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router		/storefront/recipients [post]
func (h *Handler) CreateStorefrontRecipient(w http.ResponseWriter, r *http.Request) {
	var req saveRecipientRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	recipient, err := h.svc.CreateStorefrontRecipient(r.Context(), h.recipientOwner(r), req.toDomain())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Created(w, recipient)
}

// @Summary		Update a saved recipient
// @Description	Full replace by id: every field is written as sent, so an omitted address part is cleared. The purchaser email the recipient was saved under is kept.
// @Tags			storefront
// @Accept			json
// @Produce		json
// @Param			recipientId	path	string	true	"recipientId"
// @Param			body	body	saveRecipientRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Failure		409	{object}	httpx.Envelope	"CONFLICT"
// @Router		/storefront/recipients/{recipientId} [put]
func (h *Handler) UpdateStorefrontRecipient(w http.ResponseWriter, r *http.Request) {
	var req saveRecipientRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	recipient, err := h.svc.UpdateStorefrontRecipient(r.Context(), h.recipientOwner(r).CustomerID, chi.URLParam(r, "recipientId"), req.toDomain())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, recipient)
}

// @Summary		Delete a saved recipient
// @Tags			storefront
// @Param			recipientId	path	string	true	"recipientId"
// @Success		204
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router		/storefront/recipients/{recipientId} [delete]
func (h *Handler) DeleteStorefrontRecipient(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteStorefrontRecipient(r.Context(), chi.URLParam(r, "recipientId")); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.NoContent(w)
}

// --- Public tracking (SF-32) -----------------------------------------------------

// @Summary		Track an order by its reference
// @Tags			storefront
// @Produce		json
// @Param			reference	path	string	true	"reference"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/orders/track/{reference} [get]
func (h *Handler) TrackOrder(w http.ResponseWriter, r *http.Request) {
	order, err := h.svc.GetOrderByReference(r.Context(), chi.URLParam(r, "reference"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, trackingDTO(order))
}

// multiValueQuery reads a repeated or comma-separated query parameter.
func multiValueQuery(q url.Values, key string) []string {
	var out []string
	for _, v := range q[key] {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}
