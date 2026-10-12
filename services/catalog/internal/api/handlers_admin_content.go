package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/httpx"
)

// --- Admin: content ----------------------------------------------------------------

type contentRequest struct {
	Value string `json:"value"`
	Type  string `json:"type"`
}

// @Summary		Set a site content block
// @Tags			admin-catalogue
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			key	path	string	true	"key"
// @Param			body	body	contentRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/content/{key} [patch]
func (h *Handler) SetContent(w http.ResponseWriter, r *http.Request) {
	var req contentRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.svc.SetContent(r.Context(), staffClaims(r), chi.URLParam(r, "key"), req.Value, req.Type); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"success": true})
}

// @Summary      Get site content block by key
// @Tags         admin-site-content
// @Produce      json
// @Security     BearerAuth
// @Param        key path string true "Content key"
// @Success      200 {object} httpx.Envelope
// @Failure      404 {object} httpx.Envelope "NOT_FOUND"
// @Failure      500 {object} httpx.Envelope "INTERNAL_ERROR"
// @Router       /admin/content/{key} [get]
func (h *Handler) GetContentByKey(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")

	if strings.TrimSpace(key) == "" {
		httpx.WriteError(w, r, apperr.Validation("key is required"))
		return
	}

	content, err := h.svc.GetContentByKey(r.Context(), key)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	httpx.OK(w, content)
}
