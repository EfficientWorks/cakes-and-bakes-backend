package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/httpx"
	"bakehouse/pkg/tenant"
	"bakehouse/services/identity/identityapi"
)

type staffInviteRequest struct {
	Email     string `json:"email"`
	Password  string `json:"password"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Role      string `json:"role"`
	ScopeType string `json:"scopeType"`
	// Accepted and ignored so older consoles still sending it are not
	// refused by the strict decoder; franchise scope no longer exists.
	FranchiseID *string `json:"franchiseId,omitempty"`
	BranchID    *string `json:"branchId,omitempty"`
	// KDS: the corporate office (central kitchen) for scopeType
	// "central_kitchen". Only with role central_kitchen_staff.
	CentralKitchenID *string `json:"centralKitchenId,omitempty"`
	// KDS/POS: optional 4-digit passcode set together with the account.
	Passcode string `json:"passcode,omitempty" example:"1234"`

	MaxRefundCents      *int64   `json:"maxRefundCents,omitempty"`
	MaxDiscountPct      *float64 `json:"maxDiscountPct,omitempty"`
	OrderVoidWindowMins *int     `json:"orderVoidWindowMins,omitempty"`
}

// @Summary		Invite a staff member
// @Tags			admin-staff
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body	staffInviteRequest	true	"Request body"
// @Success		201	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/admin/staff/invite [post]
func (h *Handler) InviteStaff(w http.ResponseWriter, r *http.Request) {
	var req staffInviteRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	staff, err := h.svc.CreateStaffUserWithPasscode(r.Context(), staffClaims(r), req.Email, req.Password, req.FirstName, req.LastName, identityapi.StaffRoleAssignment{
		Role: req.Role, ScopeType: req.ScopeType, BranchID: req.BranchID, CentralKitchenID: req.CentralKitchenID,
		MaxRefundCents: req.MaxRefundCents, MaxDiscountPct: req.MaxDiscountPct, OrderVoidWindowMins: req.OrderVoidWindowMins,
	}, req.Passcode)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Created(w, map[string]interface{}{"id": staff.ID, "email": staff.Email})
}

// @Summary		Deactivate a staff member
// @Tags			admin-staff
// @Produce		json
// @Security		BearerAuth
// @Param			staffId	path	string	true	"staffId"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/staff/{staffId}/deactivate [post]
func (h *Handler) DeactivateStaff(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "staffId")
	if err := h.svc.DeactivateStaffUser(r.Context(), id); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"success": true})
}

// ListStaff serves the network-wide staff directory (SA-16).
//
//	@Summary		List staff in scope
//	@Tags			admin-staff
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	httpx.Envelope
//	@Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
//	@Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
//	@Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
//	@Router			/admin/staff [get]
func (h *Handler) ListStaff(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := identityapi.StaffDirectoryFilter{
		Role:             q.Get("role"),
		BranchID:         q.Get("branchId"),
		CentralKitchenID: q.Get("centralKitchenId"),
		Search:           q.Get("search"),
	}
	if v := q.Get("isActive"); v != "" {
		b := v == "true"
		filter.IsActive = &b
	}
	list, err := h.svc.ListStaffNetwork(r.Context(), staffClaims(r), filter)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	canReveal := tenant.HasPermission(staffClaims(r), tenant.PermStaffManage)
	out := make([]staffDirectoryDTOType, 0, len(list))
	for _, e := range list {
		dto := staffDirectoryDTO(&e)
		if canReveal {
			if code := h.svc.RevealPasscode(e.PasscodeSealed); code != "" {
				dto.Passcode = &code
			}
		}
		out = append(out, dto)
	}
	httpx.OK(w, out)
}

type staffChangePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

// @Summary		Change your own staff password
// @Tags			admin-staff
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body	staffChangePasswordRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Router			/admin/staff/me/change-password [post]
func (h *Handler) ChangeStaffPassword(w http.ResponseWriter, r *http.Request) {
	var req staffChangePasswordRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	claims := staffClaims(r)
	if claims == nil {
		httpx.WriteError(w, r, apperr.Unauthenticated("Sign in again to change your password."))
		return
	}
	if err := h.svc.ChangeStaffPassword(r.Context(), claims.StaffID, req.CurrentPassword, req.NewPassword); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"changed": true})
}

type staffUpdateProfileRequest struct {
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
}

// @Summary		Update your own staff profile
// @Tags			staff-auth
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body	staffUpdateProfileRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Router			/admin/staff/me/profile [patch]
func (h *Handler) UpdateOwnStaffProfile(w http.ResponseWriter, r *http.Request) {
	var req staffUpdateProfileRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	claims := staffClaims(r)
	if claims == nil {
		httpx.WriteError(w, r, apperr.Unauthenticated("Sign in again to change your password."))
		return
	}
	if err := h.svc.UpdateOwnStaffProfile(r.Context(), claims.StaffID, req.FirstName, req.LastName, req.Email, req.Phone); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"changed": true})
}

type staffUpdateRequest struct {
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Role      string `json:"role"`
	ScopeType string `json:"scopeType"`
	// Accepted and ignored; see staffInviteRequest.
	FranchiseID *string `json:"franchiseId"`
	BranchID    *string `json:"branchId"`
	// KDS: corporate office for scopeType "central_kitchen".
	CentralKitchenID *string `json:"centralKitchenId,omitempty"`
	// KDS/POS: omit to keep, "" to remove, 4 digits to set.
	Passcode *string `json:"passcode,omitempty" example:"1234"`
}

// @Summary		Update a staff member's details, role and scope
// @Tags			admin-staff
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			staffId	path	string	true	"staffId"
// @Param			body	body	staffUpdateRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/staff/{staffId} [patch]
func (h *Handler) UpdateStaff(w http.ResponseWriter, r *http.Request) {
	var req staffUpdateRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	id := chi.URLParam(r, "staffId")
	if err := h.svc.UpdateStaffUserKDS(r.Context(), staffClaims(r), id, req.FirstName, req.LastName,
		req.Role, req.ScopeType, req.BranchID, req.CentralKitchenID, req.Passcode); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]interface{}{"success": true})
}

// @Summary		Restore access to a deactivated staff member
// @Tags			admin-staff
// @Produce		json
// @Security		BearerAuth
// @Param			staffId	path	string	true	"staffId"
// @Success		200	{object}	httpx.Envelope
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/staff/{staffId}/reactivate [post]
func (h *Handler) ReactivateStaff(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.ReactivateStaffUser(r.Context(), chi.URLParam(r, "staffId")); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]interface{}{"success": true})
}
