package api

import (
	"net/http"

	"bakehouse/pkg/httpx"
	"bakehouse/services/identity/identityapi"
)

// kdsBusinessIDRequest is step one of the KDS sign-in.
type kdsBusinessIDRequest struct {
	BusinessID string `json:"businessId" example:"ECO001"`
}

// kdsLoginRequest is step two of the KDS sign-in.
type kdsLoginRequest struct {
	BusinessID string `json:"businessId" example:"ECO001"`
	// Exactly 4 digits.
	Passcode string `json:"passcode" example:"1234"`
}

// kdsLocationDTO is the store or central kitchen a KDS signed in to.
type kdsLocationDTO struct {
	// branch | central_kitchen
	Type             string `json:"type" example:"central_kitchen"`
	BranchID         string `json:"branchId"`
	CentralKitchenID string `json:"centralKitchenId"`
	BusinessID       string `json:"businessId"`
	Name             string `json:"name"`
	Timezone         string `json:"timezone"`
	// The stores this KDS covers (one for a store; the assigned stores for a
	// central kitchen). Drives the KDS "Choose Store" picker.
	Branches []identityapi.KDSBranchRef `json:"branches"`
}

func kdsLocation(l *identityapi.KDSLocation) kdsLocationDTO {
	out := kdsLocationDTO{
		Type: l.Type, BranchID: l.BranchID, CentralKitchenID: l.CentralKitchenID,
		BusinessID: l.BusinessID, Name: l.Name, Timezone: l.Timezone, Branches: l.Branches,
	}
	if out.Branches == nil {
		out.Branches = []identityapi.KDSBranchRef{}
	}
	return out
}

// @Summary		Check a KDS Location ID before asking for a passcode
// @Description	Resolves a Location ID to a store or a central kitchen (corporate office) and says which in "type". Grants nothing. Same Location IDs as the POS; the response also lists the stores the KDS will show.
// @Tags			kds-auth
// @Accept			json
// @Produce		json
// @Param			body	body	kdsBusinessIDRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope{data=object{location=kdsLocationDTO}}
// @Failure		401	{object}	httpx.Envelope	"INVALID_BUSINESS_ID"
// @Failure		403	{object}	httpx.Envelope	"LOCATION_INACTIVE or OUTLET_SUSPENDED"
// @Failure		429	{object}	httpx.Envelope	"RATE_LIMITED"
// @Router			/kds/auth/business-id [post]
func (h *Handler) KDSValidateBusinessID(w http.ResponseWriter, r *http.Request) {
	var req kdsBusinessIDRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	loc, err := h.svc.ValidateKDSBusinessID(r.Context(), req.BusinessID, clientIP(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]any{"location": kdsLocation(loc)})
}

// @Summary		Sign in to the KDS with a Location ID and passcode
// @Description	Store Location ID: central-kitchen staff assigned to that store (sees that store only). Central-kitchen Location ID: central-kitchen staff assigned to that kitchen (sees every assigned store). Returns an ordinary staff token.
// @Tags			kds-auth
// @Accept			json
// @Produce		json
// @Param			body	body	kdsLoginRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		401	{object}	httpx.Envelope	"INVALID_CREDENTIALS or INVALID_BUSINESS_ID"
// @Failure		429	{object}	httpx.Envelope	"RATE_LIMITED"
// @Router			/kds/auth/login [post]
func (h *Handler) KDSLogin(w http.ResponseWriter, r *http.Request) {
	var req kdsLoginRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	pair, staff, roles, loc, err := h.svc.KDSLogin(r.Context(), req.BusinessID, req.Passcode, clientIP(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]any{
		"accessToken":  pair.AccessToken,
		"refreshToken": pair.RefreshToken,
		"staff":        staffDTO(staff, roles),
		"location":     kdsLocation(loc),
	})
}

// @Summary		Exchange a KDS refresh token
// @Description	Same behaviour as /staff/auth/refresh — the KDS carries a staff token.
// @Tags			kds-auth
// @Accept			json
// @Produce		json
// @Param			body	body	refreshRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Router			/kds/auth/refresh [post]
func (h *Handler) KDSRefresh(w http.ResponseWriter, r *http.Request) {
	h.StaffRefresh(w, r)
}
