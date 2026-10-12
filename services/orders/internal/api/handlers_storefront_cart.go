package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/httpx"
	"bakehouse/pkg/timex"
	"bakehouse/services/orders/internal/domain"
	"bakehouse/services/orders/ordersapi"
)

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
