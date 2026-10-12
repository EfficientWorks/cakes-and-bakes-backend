package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/httpx"
	"bakehouse/services/franchise/franchiseapi"
)

// @Summary		Get an outlet's print template
// @Description	receiptType is "receipt", "kot", or "cancel" — reads from whichever table that kind lives in.
// @Tags			pos
// @Produce		json
// @Security		BearerAuth
// @Param			receiptType	path	string	true	"receipt, kot, or cancel"
// @Param			branchId	query	string	false	"Outlet (defaults to the token's own)"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/pos/print-templates/{receiptType} [get]
func (h *Handler) GetPrintTemplate(w http.ResponseWriter, r *http.Request) {
	t, err := h.svc.GetPrintTemplate(r.Context(), staffClaims(r),
		r.URL.Query().Get("branchId"), chi.URLParam(r, "receiptType"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, t)
}

// @Summary		Update one field of an outlet's print template
// @Description	Body names one field and its new value — e.g. {"field":"showTime","value":true}. Which fields are valid depends on receiptType.
// @Tags			pos
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			receiptType	path	string							true	"receipt, kot, or cancel"
// @Param			branchId	query	string							false	"Outlet (defaults to the token's own)"
// @Param			body		body	franchiseapi.PrintTemplateFieldUpdate	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/pos/print-templates/{receiptType} [put]
func (h *Handler) UpdatePrintTemplate(w http.ResponseWriter, r *http.Request) {
	var upd franchiseapi.PrintTemplateFieldUpdate
	if err := httpx.DecodeJSON(r, &upd); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	t, err := h.svc.UpdatePrintTemplateField(r.Context(), staffClaims(r),
		r.URL.Query().Get("branchId"), chi.URLParam(r, "receiptType"), upd)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, t)
}
