package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/httpx"
	"bakehouse/services/identity/identityapi"
)

// --- Account self-service ----------------------------------------------------

// @Summary		Get the signed-in customer profile
// @Tags			account
// @Produce		json
// @Security		BearerAuth
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Router			/account/profile [get]
func (h *Handler) GetProfile(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.CustomerFromContext(r.Context())
	c, err := h.svc.GetProfile(r.Context(), claims.CustomerID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, customerDTO(c))
}

type updateProfileRequest struct {
	FirstName    string `json:"firstName"`
	LastName     string `json:"lastName"`
	MobileNumber string `json:"mobileNumber"`
}

// @Summary		Update the customer profile
// @Tags			account
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body	updateProfileRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		409	{object}	httpx.Envelope	"MOBILE_ALREADY_EXISTS — the mobile number is on another account"
// @Router			/account/profile [patch]
func (h *Handler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.CustomerFromContext(r.Context())
	var req updateProfileRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	c, err := h.svc.UpdateProfile(r.Context(), claims.CustomerID, req.FirstName, req.LastName, req.MobileNumber)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, customerDTO(c))
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
	ConfirmPassword string `json:"confirmPassword"`
}

// @Summary		Change the account password
// @Tags			account
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body	changePasswordRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Router			/account/change-password [post]
func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.CustomerFromContext(r.Context())
	var req changePasswordRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.svc.ChangePassword(r.Context(), claims.CustomerID, req.CurrentPassword, req.NewPassword, req.ConfirmPassword); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"success": true})
}

type deleteAccountRequest struct {
	Password string `json:"password"`
}

// @Summary		Delete the customer account
// @Tags			account
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body	deleteAccountRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Router			/account [delete]
func (h *Handler) DeleteAccount(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.CustomerFromContext(r.Context())
	var req deleteAccountRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.svc.DeleteAccount(r.Context(), claims.CustomerID, req.Password); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]string{"message": "Account deleted."})
}

type marketingRequest struct {
	OptIn bool `json:"optIn"`
}

// @Summary		Set marketing consent
// @Tags			account
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body	marketingRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Router			/account/marketing [patch]
func (h *Handler) SetMarketing(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.CustomerFromContext(r.Context())
	var req marketingRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.svc.SetMarketingOptIn(r.Context(), claims.CustomerID, req.OptIn, clientIP(r)); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"success": true})
}

// --- Address book --------------------------------------------------------------

// @Summary		List saved addresses
// @Tags			account
// @Produce		json
// @Security		BearerAuth
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Router			/account/addresses [get]
func (h *Handler) ListAddresses(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.CustomerFromContext(r.Context())
	addrs, err := h.svc.ListAddresses(r.Context(), claims.CustomerID)
	if err != nil {
		httpx.WriteError(w, r, apperr.Internal(err))
		return
	}
	out := make([]addressDTOType, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, addressDTO(&a))
	}
	httpx.OK(w, out)
}

type addressRequest struct {
	Label    string `json:"label"`
	Address  string `json:"address,omitempty"`
	Street   string `json:"street,omitempty"`
	City     string `json:"city"`
	State    string `json:"state"`
	Postcode string `json:"postcode"`
	Country  string `json:"country"`
}

// @Summary		Add a saved address
// @Tags			account
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body	addressRequest	true	"Request body"
// @Success		201	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Router			/account/addresses [post]
func (h *Handler) CreateAddress(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.CustomerFromContext(r.Context())
	var req addressRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	a, err := h.svc.CreateAddress(r.Context(), claims.CustomerID, addressFromRequest(req))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Created(w, addressDTO(a))
}

// @Summary		Update a saved address
// @Tags			account
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			addressId	path	string	true	"addressId"
// @Param			body	body	addressRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/account/addresses/{addressId} [patch]
func (h *Handler) UpdateAddress(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.CustomerFromContext(r.Context())
	id := chi.URLParam(r, "addressId")
	var req addressRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	a, err := h.svc.UpdateAddress(r.Context(), claims.CustomerID, id, addressFromRequest(req))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, addressDTO(a))
}

// @Summary		Delete a saved address
// @Tags			account
// @Produce		json
// @Security		BearerAuth
// @Param			addressId	path	string	true	"addressId"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/account/addresses/{addressId} [delete]
func (h *Handler) DeleteAddress(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.CustomerFromContext(r.Context())
	id := chi.URLParam(r, "addressId")
	if err := h.svc.DeleteAddress(r.Context(), claims.CustomerID, id); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"success": true})
}

// @Summary		Make an address the default
// @Tags			account
// @Produce		json
// @Security		BearerAuth
// @Param			addressId	path	string	true	"addressId"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/account/addresses/{addressId}/set-default [post]
func (h *Handler) SetDefaultAddress(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.CustomerFromContext(r.Context())
	id := chi.URLParam(r, "addressId")
	if err := h.svc.SetDefaultAddress(r.Context(), claims.CustomerID, id); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"success": true})
}

// --- Recipient book --------------------------------------------------------------

// @Summary		List saved gift recipients
// @Tags			account
// @Produce		json
// @Security		BearerAuth
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Router			/account/recipients [get]
func (h *Handler) ListRecipients(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.CustomerFromContext(r.Context())
	recipients, err := h.svc.ListRecipients(r.Context(), claims.CustomerID)
	if err != nil {
		httpx.WriteError(w, r, apperr.Internal(err))
		return
	}
	out := make([]recipientDTOType, 0, len(recipients))
	for _, rec := range recipients {
		out = append(out, recipientDTO(&rec))
	}
	httpx.OK(w, out)
}

type recipientRequest struct {
	FirstName     string                     `json:"firstName"`
	LastName      string                     `json:"lastName"`
	Phone         string                     `json:"phone"`
	Relationship  string                     `json:"relationship"`
	OccasionDates []identityapi.OccasionDate `json:"occasionDates"`
}

// @Summary		Add a saved gift recipient
// @Tags			account
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body	recipientRequest	true	"Request body"
// @Success		201	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Router			/account/recipients [post]
func (h *Handler) CreateRecipient(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.CustomerFromContext(r.Context())
	var req recipientRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	rec, err := h.svc.CreateRecipient(r.Context(), claims.CustomerID, recipientFromRequest(req))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Created(w, recipientDTO(rec))
}

// @Summary		Update a saved gift recipient
// @Tags			account
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			recipientId	path	string	true	"recipientId"
// @Param			body	body	recipientRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/account/recipients/{recipientId} [patch]
func (h *Handler) UpdateRecipient(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.CustomerFromContext(r.Context())
	id := chi.URLParam(r, "recipientId")
	var req recipientRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.svc.UpdateRecipient(r.Context(), claims.CustomerID, id, recipientFromRequest(req)); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"success": true})
}

// @Summary		Delete a saved gift recipient
// @Tags			account
// @Produce		json
// @Security		BearerAuth
// @Param			recipientId	path	string	true	"recipientId"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/account/recipients/{recipientId} [delete]
func (h *Handler) DeleteRecipient(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.CustomerFromContext(r.Context())
	id := chi.URLParam(r, "recipientId")
	if err := h.svc.DeleteRecipient(r.Context(), claims.CustomerID, id); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"success": true})
}
