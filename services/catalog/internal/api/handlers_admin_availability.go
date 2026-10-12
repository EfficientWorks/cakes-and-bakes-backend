package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/httpx"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/catalog/internal/domain"
)

// --- Admin: branch availability --------------------------------------------------

// @Summary		Get the branch availability grid
// @Tags			admin-availability
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	path	string	true	"branchId"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/branches/{branchId}/availability [get]
func (h *Handler) GetAvailabilityGrid(w http.ResponseWriter, r *http.Request) {
	date := parseDate(r.URL.Query().Get("date"))
	rows, err := h.svc.GetAvailabilityGrid(r.Context(), staffClaims(r), chi.URLParam(r, "branchId"), date)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out := make([]gridRowDTOType, 0, len(rows))
	for _, row := range rows {
		out = append(out, gridRowDTO(&row))
	}
	httpx.OK(w, out)
}

type availabilityRequest struct {
	BranchEnabled      *bool    `json:"branchEnabled"`
	Reason             string   `json:"reason"`
	AvailableDays      []string `json:"availableDays"`
	PriceOverrideCents *int64   `json:"priceOverrideCents"`
	AvailableFrom      string   `json:"availableFrom"`
	AvailableUntil     string   `json:"availableUntil"`
}

// @Summary		Set a product's weekly availability at a branch
// @Tags			admin-availability
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	path	string	true	"branchId"
// @Param			productId	path	string	true	"productId"
// @Param			body	body	availabilityRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/branches/{branchId}/products/{productId}/availability [patch]
func (h *Handler) UpdateAvailability(w http.ResponseWriter, r *http.Request) {
	var req availabilityRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	patch := domain.AvailabilityPatch{BranchEnabled: req.BranchEnabled, Reason: req.Reason, AvailableDays: req.AvailableDays, PriceOverrideCents: req.PriceOverrideCents}
	if req.AvailableFrom != "" {
		if t := parseAdminDate(req.AvailableFrom); t != nil {
			patch.AvailableFrom = t
		}
	}
	if req.AvailableUntil != "" {
		if t := parseAdminDate(req.AvailableUntil); t != nil {
			patch.AvailableUntil = t
		}
	}

	out, err := h.svc.UpdateAvailability(r.Context(), staffClaims(r), chi.URLParam(r, "branchId"), chi.URLParam(r, "productId"), patch)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, ruleDTO(out))
}

type dateOverrideRequest struct {
	Date      string `json:"date"`
	Available bool   `json:"available"`
	Reason    string `json:"reason"`
}

// @Summary		Override a product's availability for one date
// @Tags			admin-availability
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	path	string	true	"branchId"
// @Param			productId	path	string	true	"productId"
// @Param			body	body	dateOverrideRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/branches/{branchId}/products/{productId}/date-override [post]
func (h *Handler) CreateDateOverride(w http.ResponseWriter, r *http.Request) {
	var req dateOverrideRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if req.Date == "" {
		httpx.WriteError(w, r, apperr.Validation("date is required (YYYY-MM-DD)."))
		return
	}
	out, err := h.svc.AddDateOverride(r.Context(), staffClaims(r), chi.URLParam(r, "branchId"), chi.URLParam(r, "productId"),
		catalogapi.DateOverride{Date: req.Date, Available: req.Available, Reason: req.Reason})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, ruleDTO(out))
}

type bulkDisableRequest struct {
	ProductIDs []string `json:"productIds"`
	Reason     string   `json:"reason"`
}

// @Summary		Disable several products at a branch at once
// @Tags			admin-availability
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	path	string	true	"branchId"
// @Param			body	body	bulkDisableRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/branches/{branchId}/availability/bulk-disable [post]
func (h *Handler) BulkDisable(w http.ResponseWriter, r *http.Request) {
	var req bulkDisableRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	count, err := h.svc.BulkDisable(r.Context(), staffClaims(r), chi.URLParam(r, "branchId"), req.ProductIDs, req.Reason)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]int{"disabled": count})
}

type reasonOnlyRequest struct {
	Reason string `json:"reason"`
}

// @Summary		Mark a product sold out for today
// @Tags			admin-availability
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	path	string	true	"branchId"
// @Param			productId	path	string	true	"productId"
// @Param			body	body	reasonOnlyRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/branches/{branchId}/products/{productId}/sold-out [post]
func (h *Handler) SetSoldOut(w http.ResponseWriter, r *http.Request) {
	var req reasonOnlyRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.svc.SetSoldOut(r.Context(), staffClaims(r), chi.URLParam(r, "branchId"), chi.URLParam(r, "productId"), req.Reason); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"success": true})
}

// @Summary		Restore a product marked sold out today
// @Tags			admin-availability
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	path	string	true	"branchId"
// @Param			productId	path	string	true	"productId"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/branches/{branchId}/products/{productId}/restore [post]
func (h *Handler) RestoreToday(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.RestoreToday(r.Context(), staffClaims(r), chi.URLParam(r, "branchId"), chi.URLParam(r, "productId")); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"success": true})
}
