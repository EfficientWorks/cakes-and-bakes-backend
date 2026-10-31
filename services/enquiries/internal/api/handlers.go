package api

import (
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/httpx"
	"bakehouse/services/enquiries/enquiriesapi"
	"bakehouse/services/enquiries/internal/domain"
	"bakehouse/services/orders/ordersapi"
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

	// The order created with the enquiry (all optional, backward compatible).
	// date (YYYY-MM-DD) is the day it is needed; without it eventDate is
	// used, and with neither parseable only the enquiry is recorded.
	FulfilmentType string `json:"fulfilmentType" example:"pickup"`
	Date           string `json:"date" example:"2026-11-14"`
	// Preferred time, "HH:MM"; slotStart defaults to eventTime, then 09:00.
	SlotStart string `json:"slotStart" example:"14:00"`
	SlotEnd   string `json:"slotEnd" example:"14:30"`
	// Delivery address, for fulfilmentType delivery (street falls back to
	// eventLocation).
	DeliveryStreet   string `json:"deliveryStreet"`
	DeliveryCity     string `json:"deliveryCity"`
	DeliveryPostcode string `json:"deliveryPostcode" example:"E6 1AB"`
	// What the cake should say, at most 40 characters.
	CakeMessage string `json:"cakeMessage" example:"Happy 30th Priya"`
	// Up to 3 photos from POST /storefront/uploads/cake-photo (id or url).
	PhotoURLs []string `json:"photoUrls"`
	// Cake details, all optional free text, carried on the order line.
	Servings    string `json:"servings" example:"30"`
	CakeSize    string `json:"cakeSize" example:"8 inch, two tier"`
	Flavour     string `json:"flavour" example:"Red Velvet"`
	DesignNotes string `json:"designNotes" example:"Pink and gold, fresh flowers"`
}

// SubmitCustomOrder records a custom-order enquiry against a chosen store.
//
//	@Summary		Submit a custom order enquiry
//	@Description	The "Need a Custom Order?" form. Requires a store location — the enquiry is routed to that branch. When a date is given (date, else a YYYY-MM-DD eventDate) it also creates an order for that store and day: source custom_order, total 0 (price to be confirmed), unpaid, carrying cakeMessage (max 40) and up to 3 photoUrls from /storefront/uploads/cake-photo. The response is the enquiry with orderId and orderReference.
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
	for k, v := range map[string]string{
		"fulfilmentType": req.FulfilmentType, "date": req.Date, "slotStart": req.SlotStart, "slotEnd": req.SlotEnd,
		"deliveryStreet": req.DeliveryStreet, "deliveryCity": req.DeliveryCity, "deliveryPostcode": req.DeliveryPostcode,
		"cakeMessage": req.CakeMessage, "servings": req.Servings, "cakeSize": req.CakeSize,
		"flavour": req.Flavour, "designNotes": req.DesignNotes,
	} {
		if v = strings.TrimSpace(v); v != "" {
			details[k] = v
		}
	}
	if len(req.PhotoURLs) > 0 {
		details["photoUrls"] = req.PhotoURLs
	}

	order, err := customOrderFromRequest(req, message)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	h.submit(w, r, domain.SubmitRequest{
		Type:     enquiriesapi.TypeCustomOrder,
		Name:     req.Name,
		Email:    req.Email,
		Phone:    req.MobileNumber,
		Message:  message,
		BranchID: req.BranchID,
		Details:  details,
		Order:    order,
	})
}

// customOrderFromRequest is the order a custom-order form asks for, or nil
// when the form names no usable date (an older client: enquiry only).
func customOrderFromRequest(req customOrderRequest, info string) (*ordersapi.CustomOrderRequest, error) {
	var date time.Time
	if raw := strings.TrimSpace(req.Date); raw != "" {
		d, err := time.Parse("2006-01-02", raw)
		if err != nil {
			return nil, apperr.Validation("date must be YYYY-MM-DD.").
				WithFields(apperr.FieldError{Field: "date", Code: "INVALID", Message: "YYYY-MM-DD."})
		}
		date = d
	} else if d, ok := parseEventDate(req.EventDate); ok {
		date = d
	} else {
		return nil, nil
	}

	slotStart := strings.TrimSpace(req.SlotStart)
	if slotStart == "" {
		if t, err := time.Parse("15:04", strings.TrimSpace(req.EventTime)); err == nil {
			slotStart = t.Format("15:04")
		}
	}
	street := strings.TrimSpace(req.DeliveryStreet)
	if street == "" {
		street = strings.TrimSpace(req.EventLocation)
	}

	// Everything else on the form, for the kitchen and the person pricing it.
	var notes []string
	add := func(label, v string) {
		if v = strings.TrimSpace(v); v != "" {
			notes = append(notes, label+": "+v)
		}
	}
	add("Occasion", humanise(req.EventType))
	add("Event date", req.EventDate)
	add("Event time", req.EventTime)
	add("Event location", req.EventLocation)
	if req.GuestCount != nil {
		add("Guests", strconv.Itoa(*req.GuestCount))
	}
	add("Servings", req.Servings)
	add("Size", req.CakeSize)
	add("Flavour", req.Flavour)
	add("Design", req.DesignNotes)
	add("Details", info)
	notes = append(notes, "Price to be confirmed with the customer.")

	return &ordersapi.CustomOrderRequest{
		FulfilmentType: req.FulfilmentType, Date: date, SlotStart: slotStart, SlotEnd: req.SlotEnd,
		Name: req.Name, Email: req.Email, Phone: req.MobileNumber,
		Street: street, City: req.DeliveryCity, Postcode: req.DeliveryPostcode,
		Title: "Custom cake", CakeMessage: req.CakeMessage, PhotoRefs: req.PhotoURLs,
		Notes: strings.Join(notes, "\n"),
	}, nil
}

// parseEventDate reads the form's eventDate as YYYY-MM-DD or DD/MM/YYYY.
func parseEventDate(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	for _, layout := range []string{"2006-01-02", "02/01/2006"} {
		if d, err := time.Parse(layout, raw); err == nil {
			return d, true
		}
	}
	return time.Time{}, false
}

// humanise turns "baby_shower" into "Baby shower".
func humanise(v string) string {
	v = strings.TrimSpace(strings.ReplaceAll(v, "_", " "))
	if v == "" {
		return ""
	}
	return strings.ToUpper(v[:1]) + v[1:]
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

// --- Wholesale ----------------------------------------------------------------

// wholesaleRequest mirrors the "Wholesale Register" application. It is taken
// as an enquiry rather than creating an account — head office reviews it and
// replies — so there is no password and no branch.
type wholesaleRequest struct {
	FullName     string `json:"fullName"`
	Email        string `json:"email"`
	MobileNumber string `json:"mobileNumber"`
	TradeNumber  string `json:"tradeNumber" example:"12345678"`
	SortCode     string `json:"sortCode" example:"12-34-56"`
}

// sortCodePattern is a UK sort code in the shape the form formats it to.
var sortCodePattern = regexp.MustCompile(`^\d{2}-\d{2}-\d{2}$`)

// SubmitWholesale records a wholesale account application for head office.
//
//	@Summary		Submit a wholesale application
//	@Description	The "Wholesale Register" form. Recorded as a brand-level enquiry (never routed to a branch) with the trade number and sort code in details; head office reviews it and replies. No account is created.
//	@Tags			enquiries
//	@Accept			json
//	@Produce		json
//	@Param			body	body		wholesaleRequest	true	"Application"
//	@Success		201		{object}	httpx.Envelope{data=enquiriesapi.Enquiry}
//	@Failure		400		{object}	httpx.Envelope	"VALIDATION_FAILED"
//	@Failure		429		{object}	httpx.Envelope	"RATE_LIMITED"
//	@Router			/enquiries/wholesale [post]
func (h *Handler) SubmitWholesale(w http.ResponseWriter, r *http.Request) {
	var req wholesaleRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	tradeNumber := strings.TrimSpace(req.TradeNumber)
	sortCode := strings.TrimSpace(req.SortCode)

	var fields []apperr.FieldError
	if tradeNumber == "" {
		fields = append(fields, apperr.FieldError{Field: "tradeNumber", Code: "REQUIRED", Message: "Please enter your trade number."})
	}
	if !sortCodePattern.MatchString(sortCode) {
		fields = append(fields, apperr.FieldError{Field: "sortCode", Code: "INVALID", Message: "Enter the sort code as 00-00-00."})
	}
	if len(fields) > 0 {
		httpx.WriteError(w, r, apperr.Validation("Check your business details.").WithFields(fields...))
		return
	}

	h.submit(w, r, domain.SubmitRequest{
		Type:  enquiriesapi.TypeWholesale,
		Name:  req.FullName,
		Email: req.Email,
		Phone: req.MobileNumber,
		Details: map[string]any{
			"tradeNumber": tradeNumber,
			"sortCode":    sortCode,
		},
	})
}

// submit is the shared tail of the form handlers.
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
//	@Param			type		query		string	false	"custom_order, franchise, contact or wholesale"
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
