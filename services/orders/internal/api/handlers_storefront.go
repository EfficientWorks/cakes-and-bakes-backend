package api

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/httpx"
	"bakehouse/pkg/timex"
	"bakehouse/services/orders/internal/domain"
	"bakehouse/services/orders/ordersapi"
)

type Handler struct {
	svc *domain.Service
}

func NewHandler(svc *domain.Service) *Handler {
	return &Handler{svc: svc}
}

// cartIdentity resolves the caller's cart identity from either a customer
// bearer token (optional — guest checkout is always allowed, SF-29) or the
// X-Cart-Token header the client stores after its first cart action.
func (h *Handler) cartIdentity(r *http.Request) domain.CartIdentity {
	if claims, ok := auth.CustomerFromContext(r.Context()); ok {
		// The guest token too, when a just-signed-in shopper still sends it:
		// their guest cart is merged into the account's.
		return domain.CartIdentity{CustomerID: &claims.CustomerID, GuestToken: r.Header.Get("X-Cart-Token")}
	}
	return domain.CartIdentity{GuestToken: r.Header.Get("X-Cart-Token")}
}

func writeCartToken(w http.ResponseWriter, token string) {
	if token != "" {
		w.Header().Set("X-Cart-Token", token)
	}
}

// --- Slots (SF-20, GET /storefront/slots) -------------------------------------

// @Summary		List bookable delivery or pickup slots
// @Description	Slots are cut every 30 minutes from the store's pickup or delivery hours. With productId (repeatable) or cart=true, slots inside a seasonal tag's restricted dates for those products are left out; unavailableDates lists the days that lost every slot to a restriction, restrictions lists the windows.
// @Param			branchId		query	string	true	"Store"
// @Param			fulfilmentType	query	string	true	"pickup | delivery"
// @Param			startDate		query	string	false	"YYYY-MM-DD (default today)"
// @Param			days			query	int		false	"How many days (default 14)"
// @Param			productId		query	string	false	"Products whose restricted dates apply (repeatable)"
// @Param			cart			query	bool	false	"true: the restricted dates of every product in the caller's cart"
// @Tags			storefront
// @Produce		json
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/storefront/slots [get]
func (h *Handler) GetSlots(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	branchID := q.Get("branchId")
	fulfilmentType := q.Get("fulfilmentType")
	if branchID == "" || (fulfilmentType != ordersapi.FulfilmentPickup && fulfilmentType != ordersapi.FulfilmentDelivery) {
		httpx.WriteError(w, r, apperr.Validation("branchId and fulfilmentType (pickup|delivery) are required."))
		return
	}
	start := time.Now()
	if v := q.Get("startDate"); v != "" {
		if t, err := timex.ParseDate(v); err == nil {
			start = t
		}
	}
	days := 14
	if v := q.Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			days = n
		}
	}

	// Restricted dates: for products (the product page) or for every product
	// in the caller's cart ("choose a new date").
	productIDs := multiValueQuery(q, "productId")
	if q.Get("cart") == "true" {
		productIDs = append(productIDs, h.svc.CartProductIDs(r.Context(), h.cartIdentity(r))...)
	}
	daySlots, cutoffNote, info, err := h.svc.GetAvailableSlotsFor(r.Context(), branchID, fulfilmentType, start, days, productIDs)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out := make([]daySlotsDTOType, 0, len(daySlots))
	for _, d := range daySlots {
		out = append(out, daySlotsDTO(&d))
	}
	restrictions := make([]map[string]any, 0, len(info.Restrictions))
	for _, pr := range info.Restrictions {
		restrictions = append(restrictions, map[string]any{
			"productId": pr.ProductID, "tagId": pr.TagID, "tagName": pr.TagName,
			"from": pr.From.Format("2006-01-02T15:04:05"), "to": pr.To.Format("2006-01-02T15:04:05"),
			"repeatYearly": pr.RepeatYearly,
		})
	}
	httpx.OK(w, map[string]interface{}{
		"data": out, "cutoffNote": cutoffNote,
		// Dates that lose every slot to a restricted tag: "not available
		// for this product" on the calendar.
		"unavailableDates": info.UnavailableDates,
		"restrictions":     restrictions,
	})
}

// addressSearchResponse is the typed shape of a successful /address/search
// call — declared purely so Swagger renders the real field, not a bare
// "object", in the generated docs.
type addressSearchResponse struct {
	Results []ordersapi.AddressResult `json:"results"`
}

// @Summary		Search for an address
// @Description	Cache-first address finder: checks the database for this
// @Description	exact search text first; only calls the third-party
// @Description	geocoder, and caches the result, on a miss.
// @Tags			storefront
// @Produce		json
// @Param			q	query		string	true	"Free-text address search, minimum 3 characters"
// @Success		200	{object}	httpx.Envelope{data=addressSearchResponse}
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/storefront/address/search [get]
func (h *Handler) SearchAddress(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	results, err := h.svc.SearchAddress(r.Context(), q)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, addressSearchResponse{Results: results})
}

// @Summary		Search for an address (POS)
// @Tags			pos
// @Produce		json
// @Security		BearerAuth
// @Param			q	query		string	true	"Free-text address search, minimum 3 characters"
// @Success		200	{object}	httpx.Envelope{data=addressSearchResponse}
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/pos/address/search [get]
func (h *Handler) SearchAddressPOS(w http.ResponseWriter, r *http.Request) {
	h.SearchAddress(w, r)
}

// --- Public tracking (SF-32) -----------------------------------------------------

// @Summary		Track an order by its reference
// @Tags			storefront
// @Produce		json
// @Param			reference	path	string	true	"reference"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/orders/track/{reference} [get]
func (h *Handler) TrackOrder(w http.ResponseWriter, r *http.Request) {
	order, err := h.svc.GetOrderByReference(r.Context(), chi.URLParam(r, "reference"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, trackingDTO(order))
}

// multiValueQuery reads a repeated or comma-separated query parameter.
func multiValueQuery(q url.Values, key string) []string {
	var out []string
	for _, v := range q[key] {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}
