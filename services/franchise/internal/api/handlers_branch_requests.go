package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/httpx"
	"bakehouse/services/franchise/franchiseapi"
)

// --- Branch Requests ---------------------------------------------------------

type createBranchRequestBody struct {
	BranchID       string   `json:"branchId"`
	Type           string   `json:"type"`
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	FieldsToChange []string `json:"fieldsToChange"`
	ModifierType   *string  `json:"modifierType"`
	ModifierName   *string  `json:"modifierName"`
}

// @Summary     Create a branch request
// @Tags        admin-branch-requests
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       body body createBranchRequestBody true "Request body"
// @Success     201 {object} httpx.Envelope
// @Failure     400 {object} httpx.Envelope "VALIDATION_FAILED"
// @Failure     401 {object} httpx.Envelope "UNAUTHENTICATED"
// @Failure     403 {object} httpx.Envelope "PERMISSION_DENIED"
// @Router      /admin/branch-requests [post]
func (h *Handler) CreateBranchRequest(w http.ResponseWriter, r *http.Request) {
	var body createBranchRequestBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	req := franchiseapi.BranchRequest{
		BranchID:       body.BranchID,
		Type:           body.Type,
		Title:          body.Title,
		Description:    body.Description,
		FieldsToChange: body.FieldsToChange,
		ModifierType:   body.ModifierType,
		ModifierName:   body.ModifierName,
	}
	out, err := h.svc.CreateBranchRequest(r.Context(), staffClaims(r), req)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Created(w, out)
}

// @Summary     List branch requests
// @Tags        admin-branch-requests
// @Produce     json
// @Security    BearerAuth
// @Param       type        query string false "Filter by type (product, modifier, category, other)"
// @Param       status      query string false "Filter by status"
// @Param       branchId    query string false "Filter by branch ID (storeId also accepted); must be a branch the caller holds"
// @Param       search      query string false "Search by ticket code, title, description, or branch name"
// @Param       page        query int    false "Page number (default: 1)"
// @Param       limit       query int    false "Items per page (default: 20)"
// @Success     200 {object} httpx.Envelope
// @Failure     401 {object} httpx.Envelope "UNAUTHENTICATED"
// @Failure     403 {object} httpx.Envelope "PERMISSION_DENIED"
// @Router      /admin/branch-requests [get]
func (h *Handler) ListBranchRequests(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit, _ = strconv.Atoi(q.Get("per_page"))
	}
	if limit <= 0 {
		limit, _ = strconv.Atoi(q.Get("pageSize"))
	}
	if limit <= 0 {
		limit = 20
	}

	f := franchiseapi.BranchRequestFilter{
		Type:     strings.ToLower(strings.TrimSpace(q.Get("type"))),
		Status:   q.Get("status"),
		BranchID: firstNonEmpty(q.Get("branchId"), q.Get("storeId")),
		// FranchiseID: q.Get("franchiseId"),
		Search: strings.TrimSpace(q.Get("search")),
		Page:   page,
		Limit:  limit,
	}
	list, total, stats, err := h.svc.ListBranchRequests(r.Context(), staffClaims(r), f)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if list == nil {
		list = []franchiseapi.BranchRequest{}
	}

	totalPages := 0
	if limit > 0 {
		totalPages = (total + limit - 1) / limit
	}

	meta := map[string]int{
		"page":       page,
		"limit":      limit,
		"total":      total,
		"totalPages": totalPages,
	}
	httpx.OKWithMetaAndStats(w, list, meta, stats)
}

// @Summary     Get a branch request
// @Tags        admin-branch-requests
// @Produce     json
// @Security    BearerAuth
// @Param       requestId path string true "requestId"
// @Success     200 {object} httpx.Envelope
// @Failure     401 {object} httpx.Envelope "UNAUTHENTICATED"
// @Failure     403 {object} httpx.Envelope "PERMISSION_DENIED"
// @Failure     404 {object} httpx.Envelope "NOT_FOUND"
// @Router      /admin/branch-requests/{requestId} [get]
func (h *Handler) GetBranchRequest(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.GetBranchRequest(r.Context(), staffClaims(r), chi.URLParam(r, "requestId"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, out)
}

type updateBranchRequestStatusBody struct {
	// Status must be one of: approved | rejected | cancelled | completed
	Status string `json:"status"`
	// Note is an optional message, used when rejecting.
	Note      string  `json:"note,omitempty"`
	ChangedAt *string `json:"changedAt,omitempty" example:"2026-09-16T10:33:52Z"`
}

// @Summary     Update branch request status
// @Tags        admin-branch-requests
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       requestId path  string                          true  "requestId"
// @Param       body      body  updateBranchRequestStatusBody   true  "Status transition"
// @Success     200 {object} httpx.Envelope
// @Failure     400 {object} httpx.Envelope "VALIDATION_FAILED"
// @Failure     401 {object} httpx.Envelope "UNAUTHENTICATED"
// @Failure     403 {object} httpx.Envelope "PERMISSION_DENIED"
// @Failure     404 {object} httpx.Envelope "NOT_FOUND"
// @Failure     422 {object} httpx.Envelope "BRANCH_REQUEST_INVALID_STATUS"
// @Router      /admin/branch-requests/{requestId}/status [post]
func (h *Handler) UpdateBranchRequestStatus(w http.ResponseWriter, r *http.Request) {
	var body updateBranchRequestStatusBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	id := chi.URLParam(r, "requestId")
	claims := staffClaims(r)

	r, err := httpx.WithEventTime(r, "changedAt", body.ChangedAt)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	var out *franchiseapi.BranchRequest

	switch body.Status {
	case franchiseapi.RequestStatusApproved:
		out, err = h.svc.ApproveRequest(r.Context(), claims, id)
	case franchiseapi.RequestStatusRejected:
		out, err = h.svc.RejectRequest(r.Context(), claims, id, body.Note)
	case franchiseapi.RequestStatusCancelled:
		out, err = h.svc.CancelRequest(r.Context(), claims, id)
	case franchiseapi.RequestStatusCompleted:
		out, err = h.svc.CompleteRequest(r.Context(), claims, id)
	default:
		httpx.WriteError(w, r, apperr.Validation(
			"status must be one of: approved, rejected, cancelled, completed",
		))
		return
	}

	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, out)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}
