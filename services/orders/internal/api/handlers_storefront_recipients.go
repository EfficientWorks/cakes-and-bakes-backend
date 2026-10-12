package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/auth"
	"bakehouse/pkg/httpx"
	"bakehouse/services/orders/internal/domain"
)

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
