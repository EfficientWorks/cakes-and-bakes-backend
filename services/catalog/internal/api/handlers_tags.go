package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/httpx"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/catalog/internal/domain"
)

// localTimeLayout is how restricted dates travel: store-local wall clock,
// no zone ("2026-11-24T00:30:00"). Read in each store's own time zone.
const localTimeLayout = "2006-01-02T15:04:05"

type seasonalTagDTOType struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	Description *string `json:"description"`
	// active | inactive. An inactive tag is not offered as a filter and its
	// restricted dates are not enforced.
	Status string `json:"status"`
	// Restricted dates: products with this tag cannot be ordered for a
	// pickup/delivery slot from restrictedFrom to restrictedTo (store-local
	// time), every year when repeatYearly.
	Restricted     bool    `json:"restricted"`
	RestrictedFrom *string `json:"restrictedFrom" example:"2026-11-24T00:30:00"`
	RestrictedTo   *string `json:"restrictedTo" example:"2026-11-27T13:00:00"`
	RepeatYearly   bool    `json:"repeatYearly"`
	ProductCount   int     `json:"productCount"`
	CreatedAt      string  `json:"createdAt,omitempty"`
	UpdatedAt      string  `json:"updatedAt,omitempty"`
}

func seasonalTagDTO(t catalogapi.SeasonalTag) seasonalTagDTOType {
	out := seasonalTagDTOType{
		ID: t.ID, Name: t.Name, Slug: t.Slug, Description: t.Description, Status: t.Status,
		Restricted: t.Restricted, RepeatYearly: t.RepeatYearly, ProductCount: t.ProductCount,
	}
	if t.RestrictedFrom != nil {
		v := t.RestrictedFrom.Format(localTimeLayout)
		out.RestrictedFrom = &v
	}
	if t.RestrictedTo != nil {
		v := t.RestrictedTo.Format(localTimeLayout)
		out.RestrictedTo = &v
	}
	if !t.CreatedAt.IsZero() {
		out.CreatedAt = t.CreatedAt.Format(time.RFC3339)
		out.UpdatedAt = t.UpdatedAt.Format(time.RFC3339)
	}
	return out
}

func seasonalTagDTOs(tags []catalogapi.SeasonalTag) []seasonalTagDTOType {
	out := make([]seasonalTagDTOType, 0, len(tags))
	for _, t := range tags {
		out = append(out, seasonalTagDTO(t))
	}
	return out
}

func seasonalTagIDs(tags []catalogapi.SeasonalTag) []string {
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		out = append(out, t.ID)
	}
	return out
}

type seasonalTagRequest struct {
	Name        string  `json:"name" example:"Mothers Day"`
	Description *string `json:"description,omitempty"`
	// active (default) | inactive
	Status string `json:"status,omitempty" example:"active"`
	// Restricted dates; restrictedFrom/To are store-local date-times,
	// "YYYY-MM-DDTHH:MM[:SS]" (no zone).
	Restricted     bool   `json:"restricted"`
	RestrictedFrom string `json:"restrictedFrom,omitempty" example:"2026-11-24T00:30"`
	RestrictedTo   string `json:"restrictedTo,omitempty" example:"2026-11-27T13:00"`
	RepeatYearly   bool   `json:"repeatYearly"`
}

func (req seasonalTagRequest) toInput() (domain.TagInput, error) {
	in := domain.TagInput{Name: req.Name, Description: req.Description, Status: req.Status,
		Restricted: req.Restricted, RepeatYearly: req.RepeatYearly}
	if !req.Restricted {
		return in, nil
	}
	from, err := parseLocalTime(req.RestrictedFrom, "restrictedFrom")
	if err != nil {
		return in, err
	}
	to, err := parseLocalTime(req.RestrictedTo, "restrictedTo")
	if err != nil {
		return in, err
	}
	in.RestrictedFrom, in.RestrictedTo = from, to
	return in, nil
}

// parseLocalTime reads a store-local date-time. A zone, if one is sent, is
// dropped: the time is meant as the wall clock at each store.
func parseLocalTime(raw, field string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02 15:04", time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, raw); err == nil {
			w := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.UTC)
			return &w, nil
		}
	}
	return nil, apperr.Validation(field + " must be a date and time, YYYY-MM-DDTHH:MM.").
		WithFields(apperr.FieldError{Field: field, Code: "INVALID", Message: "YYYY-MM-DDTHH:MM"})
}

// @Summary		List seasonal tags
// @Tags			admin
// @Produce		json
// @Security		BearerAuth
// @Param			status	query		string	false	"active | inactive (default all)"
// @Param			search	query		string	false	"Name contains"
// @Success		200		{object}	httpx.Envelope{data=[]seasonalTagDTOType}
// @Router			/admin/seasonal-tags [get]
func (h *Handler) ListSeasonalTags(w http.ResponseWriter, r *http.Request) {
	tags, err := h.svc.ListTags(r.Context(), r.URL.Query().Get("status"), r.URL.Query().Get("search"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, seasonalTagDTOs(tags))
}

// @Summary		Get a seasonal tag
// @Tags			admin
// @Produce		json
// @Security		BearerAuth
// @Param			tagId	path		string	true	"tagId"
// @Success		200		{object}	httpx.Envelope{data=seasonalTagDTOType}
// @Router			/admin/seasonal-tags/{tagId} [get]
func (h *Handler) GetSeasonalTag(w http.ResponseWriter, r *http.Request) {
	t, err := h.svc.GetTag(r.Context(), chi.URLParam(r, "tagId"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, seasonalTagDTO(*t))
}

// @Summary		Create a seasonal tag
// @Description	A tag mapped to products from the product form. With restricted dates, its products cannot be ordered on the website for a pickup/delivery slot in that window (store-local time), every year when repeatYearly.
// @Tags			admin
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body		seasonalTagRequest	true	"Request body"
// @Success		201		{object}	httpx.Envelope{data=seasonalTagDTOType}
// @Failure		409		{object}	httpx.Envelope	"TAG_NAME_TAKEN"
// @Router			/admin/seasonal-tags [post]
func (h *Handler) CreateSeasonalTag(w http.ResponseWriter, r *http.Request) {
	var req seasonalTagRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	in, err := req.toInput()
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	t, err := h.svc.CreateTag(r.Context(), staffClaims(r), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Created(w, seasonalTagDTO(*t))
}

// @Summary		Update a seasonal tag
// @Description	Replaces the tag's fields (send them all).
// @Tags			admin
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			tagId	path		string				true	"tagId"
// @Param			body	body		seasonalTagRequest	true	"Request body"
// @Success		200		{object}	httpx.Envelope{data=seasonalTagDTOType}
// @Router			/admin/seasonal-tags/{tagId} [put]
func (h *Handler) UpdateSeasonalTag(w http.ResponseWriter, r *http.Request) {
	var req seasonalTagRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	in, err := req.toInput()
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	t, err := h.svc.UpdateTag(r.Context(), staffClaims(r), chi.URLParam(r, "tagId"), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, seasonalTagDTO(*t))
}

// @Summary		Delete a seasonal tag
// @Description	Removes the tag and unmaps it from its products.
// @Tags			admin
// @Produce		json
// @Security		BearerAuth
// @Param			tagId	path		string	true	"tagId"
// @Success		200		{object}	httpx.Envelope
// @Router			/admin/seasonal-tags/{tagId} [delete]
func (h *Handler) DeleteSeasonalTag(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteTag(r.Context(), staffClaims(r), chi.URLParam(r, "tagId")); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"deleted": true})
}

// @Summary		Seasonal tags for the storefront
// @Description	Active tags that have at least one product: the shop filter and gift finder chips. Filter products with ?tag=<id>.
// @Tags			storefront
// @Produce		json
// @Success		200	{object}	httpx.Envelope{data=[]seasonalTagDTOType}
// @Router			/storefront/seasonal-tags [get]
func (h *Handler) StorefrontSeasonalTags(w http.ResponseWriter, r *http.Request) {
	tags, err := h.svc.StorefrontTags(r.Context())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, seasonalTagDTOs(tags))
}
