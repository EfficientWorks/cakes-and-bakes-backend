package api

import (
	"net"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/httpx"
	"bakehouse/services/enquiries/enquiriesapi"
	"bakehouse/services/enquiries/internal/domain"
)

// Handler serves the three storefront forms, the newsletter footer, and the
// admin queue behind them.
type Handler struct {
	svc *domain.Service
}

// NewHandler returns a Handler for svc.
func NewHandler(svc *domain.Service) *Handler { return &Handler{svc: svc} }

func staffClaims(r *http.Request) *auth.StaffClaims {
	c, _ := auth.StaffFromContext(r.Context())
	return c
}

// clientIP records who submitted a public form, for abuse triage. It trusts
// X-Forwarded-For only for its first entry, which is what the CDN sets.
func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if host, _, err := net.SplitHostPort(fwd); err == nil {
			return host
		}
		return fwd
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// --- Custom order -------------------------------------------------------------

// customOrderRequest mirrors the "Need a Custom Order?" form.
type customOrderRequest struct {
	Name                  string  `json:"name"`
	Email                 string  `json:"email"`
	MobileNumber          string  `json:"mobileNumber"`
	EventLocation         string  `json:"eventLocation"`
	EventDate             string  `json:"eventDate" example:"2026-09-05"`
	EventTime             string  `json:"eventTime" example:"14:00"`
	EventType             string  `json:"eventType" example:"baby_shower"`
	GuestCount            *int    `json:"guestCount" example:"40"`
	BranchID              string  `json:"branchId"`
	AdditionalInformation *string `json:"additionalInformation"`
}

// SubmitCustomOrder records a custom-order enquiry against a chosen store.
//
//	@Summary		Submit a custom order enquiry
//	@Description	The "Need a Custom Order?" form. Requires a store location — the enquiry is routed to that branch and inherits its franchise. Submitting this only starts a conversation; bookings are confirmed by phone.
//	@Tags			enquiries
//	@Accept			json
//	@Produce		json
//	@Param			body	body		customOrderRequest	true	"Enquiry"
//	@Success		201		{object}	httpx.Envelope{data=enquiriesapi.Enquiry}
//	@Failure		400		{object}	httpx.Envelope	"VALIDATION_FAILED"
//	@Failure		429		{object}	httpx.Envelope	"RATE_LIMITED"
//	@Router			/enquiries/custom-order [post]
func (h *Handler) SubmitCustomOrder(w http.ResponseWriter, r *http.Request) {
	var req customOrderRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	details := map[string]any{
		"eventLocation": req.EventLocation,
		"eventDate":     req.EventDate,
		"eventTime":     req.EventTime,
		"eventType":     req.EventType,
	}
	if req.GuestCount != nil {
		details["guestCount"] = *req.GuestCount
	}

	message := ""
	if req.AdditionalInformation != nil {
		message = *req.AdditionalInformation
	}

	h.submit(w, r, domain.SubmitRequest{
		Type:     enquiriesapi.TypeCustomOrder,
		Name:     req.Name,
		Email:    req.Email,
		Phone:    req.MobileNumber,
		Message:  message,
		BranchID: req.BranchID,
		Details:  details,
	})
}

// --- Franchise ----------------------------------------------------------------

// franchiseRequest mirrors the "Own a franchise" form.
type franchiseRequest struct {
	FullName           string  `json:"fullName"`
	Email              string  `json:"email"`
	MobileNumber       string  `json:"mobileNumber"`
	InterestedLocation string  `json:"interestedLocation" example:"Tampa, FL"`
	Message            *string `json:"message"`
}

// SubmitFranchiseEnquiry records a franchise enquiry for head office.
//
//	@Summary		Submit a franchise enquiry
//	@Description	Brand-level, so it is never routed to a branch — head office answers it.
//	@Tags			enquiries
//	@Accept			json
//	@Produce		json
//	@Param			body	body		franchiseRequest	true	"Enquiry"
//	@Success		201		{object}	httpx.Envelope{data=enquiriesapi.Enquiry}
//	@Failure		400		{object}	httpx.Envelope	"VALIDATION_FAILED"
//	@Failure		429		{object}	httpx.Envelope	"RATE_LIMITED"
//	@Router			/enquiries/franchise [post]
func (h *Handler) SubmitFranchiseEnquiry(w http.ResponseWriter, r *http.Request) {
	var req franchiseRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	message := ""
	if req.Message != nil {
		message = *req.Message
	}
	h.submit(w, r, domain.SubmitRequest{
		Type:    enquiriesapi.TypeFranchise,
		Name:    req.FullName,
		Email:   req.Email,
		Phone:   req.MobileNumber,
		Message: message,
		Details: map[string]any{"interestedLocation": req.InterestedLocation},
	})
}

// --- Contact ------------------------------------------------------------------

// contactRequest mirrors the general contact form. branchId is optional: a
// question about a specific store is routed there, anything else to head office.
type contactRequest struct {
	Name         string  `json:"name"`
	Email        string  `json:"email"`
	MobileNumber string  `json:"mobileNumber"`
	Subject      string  `json:"subject"`
	Message      string  `json:"message"`
	BranchID     *string `json:"branchId"`
}

// SubmitContact records a general contact enquiry.
//
//	@Summary		Submit a contact enquiry
//	@Description	General contact form. Supplying branchId routes it to that store; otherwise head office answers.
//	@Tags			enquiries
//	@Accept			json
//	@Produce		json
//	@Param			body	body		contactRequest	true	"Enquiry"
//	@Success		201		{object}	httpx.Envelope{data=enquiriesapi.Enquiry}
//	@Failure		400		{object}	httpx.Envelope	"VALIDATION_FAILED"
//	@Failure		429		{object}	httpx.Envelope	"RATE_LIMITED"
//	@Router			/enquiries/contact [post]
func (h *Handler) SubmitContact(w http.ResponseWriter, r *http.Request) {
	var req contactRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	branchID := ""
	if req.BranchID != nil {
		branchID = *req.BranchID
	}
	h.submit(w, r, domain.SubmitRequest{
		Type:     enquiriesapi.TypeContact,
		Name:     req.Name,
		Email:    req.Email,
		Phone:    req.MobileNumber,
		Message:  req.Message,
		BranchID: branchID,
		Details:  map[string]any{"subject": req.Subject},
	})
}

// submit is the shared tail of the three form handlers.
func (h *Handler) submit(w http.ResponseWriter, r *http.Request, req domain.SubmitRequest) {
	req.SourceIP = clientIP(r)
	created, err := h.svc.Submit(r.Context(), req)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Created(w, created)
}

// --- Newsletter ---------------------------------------------------------------

type newsletterRequest struct {
	Email string `json:"email"`
	// Source records where the sign-up came from, so a footer subscribe can be
	// told apart from a checkout opt-in.
	Source string `json:"source" example:"footer"`
}

// Subscribe records newsletter consent.
//
//	@Summary		Subscribe to the newsletter
//	@Description	Records marketing consent with its timestamp. Re-subscribing a previously unsubscribed address revives it rather than creating a duplicate.
//	@Tags			enquiries
//	@Accept			json
//	@Produce		json
//	@Param			body	body		newsletterRequest	true	"Email"
//	@Success		201		{object}	httpx.Envelope{data=enquiriesapi.NewsletterSubscriber}
//	@Failure		400		{object}	httpx.Envelope	"VALIDATION_FAILED"
//	@Router			/newsletter/subscribe [post]
func (h *Handler) Subscribe(w http.ResponseWriter, r *http.Request) {
	var req newsletterRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	sub, err := h.svc.Subscribe(r.Context(), req.Email, req.Source)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Created(w, sub)
}

// Unsubscribe withdraws newsletter consent.
//
//	@Summary		Unsubscribe from the newsletter
//	@Description	Always succeeds for a well-formed address, whether or not it was subscribed, so the endpoint cannot be used to discover who is on the list.
//	@Tags			enquiries
//	@Accept			json
//	@Produce		json
//	@Param			body	body		newsletterRequest	true	"Email"
//	@Success		200		{object}	httpx.Envelope
//	@Failure		400		{object}	httpx.Envelope	"VALIDATION_FAILED"
//	@Router			/newsletter/unsubscribe [post]
func (h *Handler) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	var req newsletterRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.svc.Unsubscribe(r.Context(), req.Email); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"unsubscribed": true})
}

// --- Admin queue ---------------------------------------------------------------

// ListEnquiries returns the enquiry queue.
//
//	@Summary		List enquiries
//	@Description	Brand-scoped staff see every enquiry; branch-scoped staff see only their own branch's. Scope comes from the token, not the query string.
//	@Tags			admin-enquiries
//	@Produce		json
//	@Security		BearerAuth
//	@Param			type		query		string	false	"custom_order, franchise or contact"
//	@Param			status		query		string	false	"new, in_progress or closed"
//	@Param			branchId	query		string	false	"Restrict to one branch you have access to"
//	@Param			search		query		string	false	"Partial match on name or email"
//	@Param			page		query		int		false	"1-based page number"
//	@Param			limit		query		int		false	"Page size (default 50, max 200)"
//	@Success		200			{object}	httpx.Envelope{data=[]enquiriesapi.Enquiry,meta=httpx.Meta}
//	@Failure		401			{object}	httpx.Envelope	"UNAUTHENTICATED"
//	@Failure		403			{object}	httpx.Envelope	"PERMISSION_DENIED"
//	@Router			/admin/enquiries [get]
func (h *Handler) ListEnquiries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))

	filter := enquiriesapi.EnquiryFilter{
		Type:     q.Get("type"),
		Status:   q.Get("status"),
		BranchID: q.Get("branchId"),
		Search:   q.Get("search"),
		Page:     page,
		Limit:    limit,
	}.Normalise()

	list, total, err := h.svc.List(r.Context(), staffClaims(r), filter)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if list == nil {
		list = []enquiriesapi.Enquiry{}
	}

	totalPages := 0
	if filter.Limit > 0 {
		totalPages = (total + filter.Limit - 1) / filter.Limit
	}
	httpx.OKWithMeta(w, list, httpx.Meta{
		Page: filter.Page, PerPage: filter.Limit, Total: total, TotalPages: totalPages,
	})
}

// GetEnquiry returns one enquiry.
//
//	@Summary		Get an enquiry
//	@Tags			admin-enquiries
//	@Produce		json
//	@Security		BearerAuth
//	@Param			enquiryId	path		string	true	"Enquiry id"
//	@Success		200			{object}	httpx.Envelope{data=enquiriesapi.Enquiry}
//	@Failure		401			{object}	httpx.Envelope	"UNAUTHENTICATED"
//	@Failure		403			{object}	httpx.Envelope	"PERMISSION_DENIED"
//	@Failure		404			{object}	httpx.Envelope	"NOT_FOUND"
//	@Router			/admin/enquiries/{enquiryId} [get]
func (h *Handler) GetEnquiry(w http.ResponseWriter, r *http.Request) {
	e, err := h.svc.Get(r.Context(), staffClaims(r), chi.URLParam(r, "enquiryId"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, e)
}

type updateEnquiryRequest struct {
	Status    string  `json:"status" example:"in_progress"`
	StaffNote *string `json:"staffNote"`
}

// UpdateEnquiry moves an enquiry through its lifecycle.
//
//	@Summary		Update an enquiry
//	@Description	Sets the status and optionally appends a staff note. Moving off "new" records who handled it and when.
//	@Tags			admin-enquiries
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			enquiryId	path		string					true	"Enquiry id"
//	@Param			body		body		updateEnquiryRequest	true	"New status"
//	@Success		200			{object}	httpx.Envelope{data=enquiriesapi.Enquiry}
//	@Failure		400			{object}	httpx.Envelope	"VALIDATION_FAILED"
//	@Failure		401			{object}	httpx.Envelope	"UNAUTHENTICATED"
//	@Failure		403			{object}	httpx.Envelope	"PERMISSION_DENIED"
//	@Failure		404			{object}	httpx.Envelope	"NOT_FOUND"
//	@Router			/admin/enquiries/{enquiryId} [patch]
func (h *Handler) UpdateEnquiry(w http.ResponseWriter, r *http.Request) {
	var req updateEnquiryRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if req.Status == "" {
		httpx.WriteError(w, r, apperr.Validation("status is required."))
		return
	}
	updated, err := h.svc.UpdateStatus(r.Context(), staffClaims(r), chi.URLParam(r, "enquiryId"), req.Status, req.StaffNote)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, updated)
}
