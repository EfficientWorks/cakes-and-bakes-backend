package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/httpx"
)

// --- Staff auth ---------------------------------------------------------------

type staffLoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type posBusinessIDRequest struct {
	BusinessID string `json:"businessId"`
}

type posLoginRequest struct {
	BusinessID string `json:"businessId"`
	Passcode   string `json:"passcode"`
}

// @Summary		Check a Business ID before asking for a passcode
// @Description	Step one of the till sign-in. Grants nothing — it only confirms the code and names the outlet, so the passcode screen can show which shop it is signing in to.
// @Tags			pos
// @Accept			json
// @Produce		json
// @Param			body	body	posBusinessIDRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		401	{object}	httpx.Envelope	"INVALID_BUSINESS_ID"
// @Failure		429	{object}	httpx.Envelope	"RATE_LIMITED"
// @Router			/pos/auth/business-id [post]
func (h *Handler) POSValidateBusinessID(w http.ResponseWriter, r *http.Request) {
	var req posBusinessIDRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	outlet, err := h.svc.ValidateBusinessID(r.Context(), req.BusinessID, clientIP(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	// The same "outlet" object the login below returns. Step one and step two
	// describe the same shop, and a client that has to parse two shapes for it
	// will eventually parse one of them wrong.
	httpx.OK(w, map[string]interface{}{
		"outlet": map[string]string{
			"branchId":   outlet.BranchID,
			"businessId": outlet.BusinessID,
			"name":       outlet.Name,
			// KDS flow: "branch" or "central_kitchen". A till only signs in to
			// a branch; POS login refuses a central kitchen's Location ID.
			"type":             outlet.Type,
			"centralKitchenId": outlet.CentralKitchenID,
		},
	})
}

type staffPasscodeRequest struct {
	// Empty takes POS access away, which is why it is not required.
	Passcode string `json:"passcode"`
}

// @Summary		Sign in to a till with a Business ID and passcode
// @Tags			pos
// @Accept			json
// @Produce		json
// @Param			body	body	posLoginRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		401	{object}	httpx.Envelope	"INVALID_CREDENTIALS"
// @Failure		429	{object}	httpx.Envelope	"RATE_LIMITED"
// @Router			/pos/auth/login [post]
func (h *Handler) POSLogin(w http.ResponseWriter, r *http.Request) {
	var req posLoginRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	pair, staff, roles, outlet, err := h.svc.POSLogin(r.Context(), req.BusinessID, req.Passcode, clientIP(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]interface{}{
		"accessToken":  pair.AccessToken,
		"refreshToken": pair.RefreshToken,
		"staff":        staffDTO(staff, roles),
		// The till writes every order against these, so it is told them once
		// at sign-in rather than having to ask.
		"outlet": map[string]interface{}{
			"branchId":   outlet.BranchID,
			"businessId": outlet.BusinessID,
			"name":       outlet.Name,
			"type":       outlet.Type,
			"tax": map[string]interface{}{
				"name":    outlet.TaxName,
				"ratePct": outlet.TaxRatePct, // null when no tax is set
			},
		},
	})
}

// @Summary		Set or clear a staff member's POS passcode
// @Tags			admin-staff
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			staffId	path	string	true	"staffId"
// @Param			body	body	staffPasscodeRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		409	{object}	httpx.Envelope	"PASSCODE_TAKEN"
// @Router			/admin/staff/{staffId}/passcode [put]
func (h *Handler) SetStaffPasscode(w http.ResponseWriter, r *http.Request) {
	var req staffPasscodeRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.svc.SetStaffPasscode(r.Context(), staffClaims(r), chi.URLParam(r, "staffId"), req.Passcode); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"ok": true})
}

// @Summary		Log in as staff
// @Tags			staff-auth
// @Accept			json
// @Produce		json
// @Param			body	body	staffLoginRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/staff/auth/login [post]
func (h *Handler) StaffLogin(w http.ResponseWriter, r *http.Request) {
	var req staffLoginRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	pair, staff, roles, err := h.svc.StaffLogin(r.Context(), req.Email, req.Password, clientIP(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]interface{}{
		"accessToken":  pair.AccessToken,
		"refreshToken": pair.RefreshToken,
		"staff":        staffDTO(staff, roles),
	})
}

// @Summary		Exchange a staff refresh token
// @Tags			staff-auth
// @Accept			json
// @Produce		json
// @Param			body	body	refreshRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/staff/auth/refresh [post]
func (h *Handler) StaffRefresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	pair, err := h.svc.RefreshStaff(r.Context(), req.RefreshToken)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]interface{}{"accessToken": pair.AccessToken, "refreshToken": pair.RefreshToken})
}

// @Summary		Exchange a POS refresh token
// @Description	Same behaviour as /staff/auth/refresh — the till carries a staff token.
// @Tags			pos
// @Accept			json
// @Produce		json
// @Param			body	body	refreshRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/pos/auth/refresh [post]
func (h *Handler) POSRefresh(w http.ResponseWriter, r *http.Request) {
	h.StaffRefresh(w, r)
}

// @Summary		Revoke a staff refresh token
// @Tags			staff-auth
// @Accept			json
// @Produce		json
// @Param			body	body	refreshRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/staff/auth/logout [post]
func (h *Handler) StaffLogout(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = h.svc.LogoutStaff(r.Context(), req.RefreshToken)
	httpx.OK(w, map[string]bool{"success": true})
}
