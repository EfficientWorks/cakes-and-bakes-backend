package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/httpx"
	"bakehouse/pkg/strx"
	"bakehouse/services/franchise/franchiseapi"
	"bakehouse/services/franchise/internal/domain"
)

type Handler struct {
	svc *domain.Service
}

func NewHandler(svc *domain.Service) *Handler { return &Handler{svc: svc} }

func staffClaims(r *http.Request) *auth.StaffClaims {
	c, _ := auth.StaffFromContext(r.Context())
	return c
}

type reasonRequest struct {
	Reason string `json:"reason"`
}

// --- Branches ------------------------------------------------------------------

type branchRequest struct {
	Name string `json:"name"`
	Code string `json:"code"`
	// corporate_store or franchise_store. Defaults to franchise_store.
	OwnershipType string `json:"ownershipType" example:"corporate_store"`
	// Deprecated alias for ownershipType, still read so older consoles keep
	// working. ownershipType wins when both are sent.
	Type                  string                     `json:"type"`
	Street                string                     `json:"street"`
	City                  string                     `json:"city"`
	State                 string                     `json:"state"`
	Postcode              string                     `json:"postcode"`
	Country               string                     `json:"country"`
	Latitude              *float64                   `json:"latitude"`
	Longitude             *float64                   `json:"longitude"`
	Phone                 string                     `json:"phone"`
	Email                 string                     `json:"email"`
	BusinessID            string                     `json:"businessId"`
	Timezone              string                     `json:"timezone"`
	OpeningHours          []franchiseapi.OpeningHour `json:"openingHours"`
	FulfilmentTypes       []string                   `json:"fulfilmentTypes"`
	DeliveryZone          *franchiseapi.DeliveryZone `json:"deliveryZone"`
	DeliveryFeeCents      int64                      `json:"deliveryFeeCents"`
	MinOrderCents         int64                      `json:"minOrderCents"`
	TaxName               string                     `json:"taxName"`
	TaxRatePct            *float64                   `json:"taxRatePct"`
	AcceptsCashOnDelivery bool                       `json:"acceptsCashOnDelivery"`
	CODMaxOrderCents      *int64                     `json:"codMaxOrderCents"`
	// Website card providers ("stripe"); omitted means Stripe.
	CardProviders []string `json:"cardProviders,omitempty"`
	// POS till: cash (omitted: same as acceptsCashOnDelivery) and payment-link
	// card providers (omitted: Stripe).
	PosAcceptsCash   *bool    `json:"posAcceptsCash,omitempty"`
	PosCardProviders []string `json:"posCardProviders,omitempty"`
	// Pickup / delivery hours; omitted, slots use openingHours.
	PickupHours         []franchiseapi.DayHours `json:"pickupHours,omitempty"`
	DeliveryHours       []franchiseapi.DayHours `json:"deliveryHours,omitempty"`
	SameFulfilmentHours *bool                   `json:"sameFulfilmentHours,omitempty"`
	// Delivery region and charges; omitted keeps the flat deliveryFeeCents.
	DeliveryPricing *franchiseapi.DeliveryPricing `json:"deliveryPricing,omitempty"`
}

// @Summary		Create a branch
// @Tags			admin-branches
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body	branchRequest	true	"Request body"
// @Success		201	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/admin/branches [post]
func (h *Handler) CreateBranch(w http.ResponseWriter, r *http.Request) {
	var req branchRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	b, warnings, err := h.svc.CreateBranch(r.Context(), staffClaims(r), branchFromRequest(req))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Created(w, map[string]interface{}{"branch": branchDTO(b), "warnings": warnings})
}

// @Summary		List branches in scope
// @Tags			admin-branches
// @Produce		json
// @Security		BearerAuth
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/admin/branches [get]
func (h *Handler) ListBranches(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.ListBranches(r.Context(), staffClaims(r), r.URL.Query().Get("status"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out := make([]branchDTOType, 0, len(list))
	for _, b := range list {
		out = append(out, branchDTO(&b))
	}
	httpx.OKWithMeta(w, out, map[string]int{"total": len(out)})
}

// @Summary		Get one branch
// @Tags			admin-branches
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	path	string	true	"branchId"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/branches/{branchId} [get]
func (h *Handler) GetBranch(w http.ResponseWriter, r *http.Request) {
	b, err := h.svc.GetBranch(r.Context(), staffClaims(r), chi.URLParam(r, "branchId"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, branchDTO(b))
}

// @Summary		Update a branch (governance decides which fields you may set)
// @Tags			admin-branches
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	path	string	true	"branchId"
// @Param			body	body	branchPatchRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/branches/{branchId} [patch]
func (h *Handler) UpdateBranch(w http.ResponseWriter, r *http.Request) {
	var req branchPatchRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	b, warnings, err := h.svc.UpdateBranch(r.Context(), staffClaims(r), chi.URLParam(r, "branchId"), req.toPatch())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]interface{}{"branch": branchDTO(b), "warnings": warnings})
}

// @Summary		Suspend a branch
// @Tags			admin-branches
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	path	string	true	"branchId"
// @Param			body	body	reasonRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/branches/{branchId}/suspend [post]
func (h *Handler) SuspendBranch(w http.ResponseWriter, r *http.Request) {
	var req reasonRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.svc.SuspendBranch(r.Context(), staffClaims(r), chi.URLParam(r, "branchId"), req.Reason); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"success": true})
}

// @Summary		Reactivate a suspended branch
// @Tags			admin-branches
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	path	string	true	"branchId"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/branches/{branchId}/reactivate [post]
func (h *Handler) ReactivateBranch(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.ReactivateBranch(r.Context(), staffClaims(r), chi.URLParam(r, "branchId")); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"success": true})
}

// @Summary		Close a branch immediately for the rest of today
// @Tags			admin-branches
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	path	string	true	"branchId"
// @Param			body	body	reasonRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/branches/{branchId}/emergency-close [post]
func (h *Handler) EmergencyClose(w http.ResponseWriter, r *http.Request) {
	var req reasonRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	affectedOrders, err := h.svc.EmergencyClose(r.Context(), staffClaims(r), chi.URLParam(r, "branchId"), req.Reason)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]interface{}{"success": true, "affectedOrders": affectedOrders})
}

func branchFromRequest(req branchRequest) franchiseapi.Branch {
	zone := franchiseapi.DeliveryZone{Type: "zip_list"}
	if req.DeliveryZone != nil {
		zone = *req.DeliveryZone
	}
	// Normalised and validated in the domain layer; empty means the default.
	ownershipType := req.OwnershipType
	if ownershipType == "" {
		ownershipType = req.Type
	}
	country := req.Country
	if country == "" {
		country = "US"
	}
	var taxName *string
	if req.TaxName != "" {
		taxName = &req.TaxName
	}
	posCash := req.AcceptsCashOnDelivery
	if req.PosAcceptsCash != nil {
		posCash = *req.PosAcceptsCash
	}
	return franchiseapi.Branch{
		Name: req.Name, Code: req.Code,
		Type: franchiseapi.BranchTypeRetail, OwnershipType: ownershipType,
		Street: req.Street, City: req.City, State: req.State, Postcode: req.Postcode, Country: country,
		Latitude: req.Latitude, Longitude: req.Longitude,
		Phone: strx.NilIfEmpty(req.Phone), Email: strx.NilIfEmpty(req.Email),
		Timezone: req.Timezone, OpeningHours: req.OpeningHours,
		FulfilmentTypes: req.FulfilmentTypes, DeliveryZone: zone,
		DeliveryFeeCents: req.DeliveryFeeCents, MinOrderCents: req.MinOrderCents,
		TaxName: taxName, TaxRatePct: req.TaxRatePct,
		AcceptsCashOnDelivery: req.AcceptsCashOnDelivery, CODMaxOrderCents: req.CODMaxOrderCents,
		CardProviders:    req.CardProviders,
		PosAcceptsCash:   posCash,
		PosCardProviders: req.PosCardProviders,
		PickupHours:      req.PickupHours, DeliveryHours: req.DeliveryHours,
		SameFulfilmentHours: req.SameFulfilmentHours == nil || *req.SameFulfilmentHours,
		DeliveryPricing:     derefPricing(req.DeliveryPricing),
		LocationCode:        strx.NilIfEmpty(req.BusinessID),
	}
}

type branchPatchRequest struct {
	Name     *string `json:"name,omitempty"`
	Code     *string `json:"code,omitempty"`
	Street   *string `json:"street,omitempty"`
	City     *string `json:"city,omitempty"`
	State    *string `json:"state,omitempty"`
	Postcode *string `json:"postcode,omitempty"`
	Country  *string `json:"country,omitempty"`
	Phone    *string `json:"phone,omitempty"`
	Email    *string `json:"email,omitempty"`
	// All three are applied now. They were accepted and dropped, which is why
	// changing an outlet's timezone, its franchise or its status from the form
	// appeared to save and changed nothing.
	//
	// Status still goes through the same transition the dedicated endpoints
	// use, so closing an outlet from the form is the same event as closing it
	// from the button.
	Status     *string `json:"status,omitempty"`
	BusinessID *string `json:"businessId,omitempty"`
	// corporate_store or franchise_store.
	OwnershipType *string `json:"ownershipType,omitempty" example:"franchise_store"`
	// Deprecated alias for ownershipType; ownershipType wins when both are sent.
	LegacyType            *string                     `json:"type,omitempty"`
	Timezone              *string                     `json:"timezone,omitempty"`
	FulfilmentTypes       *[]string                   `json:"fulfilmentTypes,omitempty"`
	DeliveryZone          *franchiseapi.DeliveryZone  `json:"deliveryZone,omitempty"`
	DeliveryFeeCents      *int64                      `json:"deliveryFeeCents,omitempty"`
	MinOrderCents         *int64                      `json:"minOrderCents,omitempty"`
	OpeningHours          *[]franchiseapi.OpeningHour `json:"openingHours,omitempty"`
	TaxName               *string                     `json:"taxName,omitempty"`
	TaxRatePct            *float64                    `json:"taxRatePct,omitempty"`
	AcceptsCashOnDelivery *bool                       `json:"acceptsCashOnDelivery,omitempty"`
	CODMaxOrderCents      *int64                      `json:"codMaxOrderCents,omitempty"`
	// Replaces the store's website card providers ("stripe"); [] = none.
	CardProviders *[]string `json:"cardProviders,omitempty"`
	// POS till: cash on/off, and its payment-link card providers ([] = none).
	PosAcceptsCash   *bool     `json:"posAcceptsCash,omitempty"`
	PosCardProviders *[]string `json:"posCardProviders,omitempty"`
	// Replace the pickup / delivery hours whole; [] = back to openingHours.
	PickupHours         *[]franchiseapi.DayHours `json:"pickupHours,omitempty"`
	DeliveryHours       *[]franchiseapi.DayHours `json:"deliveryHours,omitempty"`
	SameFulfilmentHours *bool                    `json:"sameFulfilmentHours,omitempty"`
	// Replaces the delivery region and charges whole.
	DeliveryPricing *franchiseapi.DeliveryPricing `json:"deliveryPricing,omitempty"`
}

func (req branchPatchRequest) toPatch() franchiseapi.BranchPatch {
	if req.OwnershipType == nil {
		req.OwnershipType = req.LegacyType
	}
	return franchiseapi.BranchPatch{
		Name: req.Name, Phone: req.Phone, Email: req.Email,
		Code: req.Code, Street: req.Street, City: req.City,
		State: req.State, Postcode: req.Postcode, Country: req.Country,
		Timezone: req.Timezone, OwnershipType: req.OwnershipType, LocationCode: req.BusinessID, Status: req.Status,
		FulfilmentTypes: req.FulfilmentTypes, DeliveryZone: req.DeliveryZone,
		DeliveryFeeCents: req.DeliveryFeeCents, MinOrderCents: req.MinOrderCents, OpeningHours: req.OpeningHours,
		TaxName: req.TaxName, TaxRatePct: req.TaxRatePct,
		AcceptsCashOnDelivery: req.AcceptsCashOnDelivery, CODMaxOrderCents: req.CODMaxOrderCents,
		DeliveryPricing: req.DeliveryPricing,
		CardProviders:   req.CardProviders,
		PosAcceptsCash:  req.PosAcceptsCash, PosCardProviders: req.PosCardProviders,
		PickupHours: req.PickupHours, DeliveryHours: req.DeliveryHours,
		SameFulfilmentHours: req.SameFulfilmentHours,
	}
}

// --- Branch exceptions -----------------------------------------------------------

// @Summary		List branch closure and short-hours exceptions
// @Tags			admin-branches
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	path	string	true	"branchId"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/branches/{branchId}/exceptions [get]
func (h *Handler) ListExceptions(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.ListExceptions(r.Context(), staffClaims(r), chi.URLParam(r, "branchId"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out := make([]exceptionDTOType, 0, len(list))
	for _, e := range list {
		out = append(out, exceptionDTO(&e))
	}
	httpx.OK(w, out)
}

type exceptionRequest struct {
	Date      string  `json:"date"`
	Reason    string  `json:"reason"`
	IsClosed  bool    `json:"isClosed"`
	OpenTime  *string `json:"openTime"`
	CloseTime *string `json:"closeTime"`
}

// @Summary		Add a branch closure or short-hours exception
// @Tags			admin-branches
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	path	string	true	"branchId"
// @Param			body	body	exceptionRequest	true	"Request body"
// @Success		201	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/branches/{branchId}/exceptions [post]
func (h *Handler) CreateException(w http.ResponseWriter, r *http.Request) {
	var req exceptionRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	date, err := time.Parse("2006-01-02", req.Date)
	if err != nil {
		httpx.WriteError(w, r, apperr.Validation("date must be YYYY-MM-DD.").
			WithFields(apperr.FieldError{Field: "date", Code: "INVALID_FORMAT", Message: "Expected YYYY-MM-DD."}))
		return
	}
	out, err := h.svc.AddException(r.Context(), staffClaims(r), franchiseapi.BranchException{
		BranchID: chi.URLParam(r, "branchId"), Date: date, Reason: req.Reason,
		IsClosed: req.IsClosed, OpenTime: req.OpenTime, CloseTime: req.CloseTime,
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Created(w, exceptionDTO(out))
}

// @Summary		Delete a branch exception
// @Tags			admin-branches
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	path	string	true	"branchId"
// @Param			exceptionId	path	string	true	"exceptionId"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/branches/{branchId}/exceptions/{exceptionId} [delete]
func (h *Handler) DeleteException(w http.ResponseWriter, r *http.Request) {
	err := h.svc.DeleteException(r.Context(), staffClaims(r), chi.URLParam(r, "branchId"), chi.URLParam(r, "exceptionId"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"success": true})
}

// --- Governance --------------------------------------------------------------------

// @Summary		List governance rules (which level owns which field)
// @Tags			admin-governance
// @Produce		json
// @Security		BearerAuth
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/admin/governance [get]
func (h *Handler) ListGovernance(w http.ResponseWriter, r *http.Request) {
	rules, err := h.svc.ListGovernanceRules(r.Context())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out := make([]governanceRuleDTOType, 0, len(rules))
	for _, g := range rules {
		out = append(out, governanceRuleDTOType{FieldKey: g.FieldKey, Level: g.Level, Description: g.Description})
	}
	httpx.OK(w, out)
}

// --- Storefront: delivery eligibility (public) --------------------------------------

type checkDeliveryRequest struct {
	Postcode string `json:"postcode"`
	// Address is the street address, when known. Stores that deliver by
	// radius measure from it; without it the ZIP's centre is used.
	Address string `json:"address,omitempty" example:"180 Lafayette Ave, Edison, NJ"`
	// TimeSlot is the delivery slot's start ("HH:MM" or "HH:MM:SS",
	// store-local), when chosen. Stores that charge by delivery time price
	// from it; without it their regular charge is quoted.
	TimeSlot string `json:"timeSlot,omitempty" example:"13:00:00"`
	// SubtotalCents is the cart's item subtotal, when known: what "free over
	// $X" rules compare against. Without it no free-over is applied.
	SubtotalCents int64 `json:"subtotalCents,omitempty" example:"6800"`
}

// @Summary		Check whether an address is deliverable
// @Tags			storefront
// @Accept			json
// @Produce		json
// @Param			body	body	checkDeliveryRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/storefront/check-delivery [post]
func (h *Handler) CheckDelivery(w http.ResponseWriter, r *http.Request) {
	var req checkDeliveryRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	result, err := h.svc.CheckDelivery(r.Context(), req.Postcode, req.Address, req.TimeSlot, req.SubtotalCents)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if result.Eligible {
		// Both shapes: `branch` is the recommendation, `branches` is every
		// outlet that covers the address so the customer can choose.
		// Each carries its deliveryQuote: the fee for this address before the
		// order size is known.
		// Nearest first, each with its distance from the address (miles) and
		// its deliveryQuote: the fee for this address before the order size
		// is known. Render in this order.
		distance := map[string]*float64{}
		for _, r := range result.Ranked {
			distance[r.ID] = r.DistanceMiles
		}
		withQuote := func(b *franchiseapi.Branch) branchWithDistanceDTOType {
			d := branchWithDistanceDTOType{branchDTOType: branchDTO(b), DistanceMiles: roundMiles(distance[b.ID])}
			if q, ok := result.Quotes[b.ID]; ok {
				d.DeliveryQuote = &q
			}
			return d
		}
		all := make([]branchWithDistanceDTOType, 0, len(result.Branches))
		for i := range result.Branches {
			all = append(all, withQuote(&result.Branches[i]))
		}
		httpx.OK(w, map[string]interface{}{
			"eligible": true,
			"branch":   withQuote(result.Branch),
			"branches": all,
		})
		return
	}
	nearest := make([]branchWithDistanceDTOType, 0, len(result.NearestBranches))
	for _, b := range result.NearestBranches {
		nearest = append(nearest, branchWithDistanceDTO(&b))
	}
	httpx.OK(w, map[string]interface{}{
		"eligible":           false,
		"nearestBranches":    nearest,
		"emailCapturePrompt": result.EmailCapturePrompt,
	})
}

// ListPickupBranches backs the pickup tab of the storefront's "Start your
// order" dialog, which lists every outlet before it has been given an
// address — so postcode is optional. Supplied, it ranks the list nearest
// first and fills in distanceMiles; omitted, the same branches come back in
// code order with distanceMiles null. The domain already degrades this way
// (rankByDistance falls back to code order whenever geocoding fails), so an
// absent postcode is the same path as an unrecognised one.
//
//	@Summary		List branches available for pickup
//	@Tags			storefront
//	@Produce		json
//	@Param			postcode	query		string	false	"Rank nearest-first and populate distanceMiles. Omit (with address) to list every pickup store by name."
//	@Param			address		query		string	false	"The customer's street address; measured from instead of the ZIP's centre when given."
//	@Param			type		query		string	false	"Filter by ownership type: corporate_store or franchise_store ('own' and 'franchise' also accepted). Omit for all branches."
//	@Success		200			{object}	httpx.Envelope{data=[]branchWithDistanceDTOType}
//	@Failure		400			{object}	httpx.Envelope	"VALIDATION_FAILED"
//	@Router			/storefront/branches [get]
func (h *Handler) ListPickupBranches(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ranked, err := h.svc.ListPickupBranches(r.Context(), q.Get("postcode"), q.Get("address"), q.Get("type"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out := make([]branchWithDistanceDTOType, 0, len(ranked))
	for _, b := range ranked {
		out = append(out, branchWithDistanceDTO(&b))
	}
	httpx.OK(w, out)
}

type zoneCoverageRequest struct {
	Email    string `json:"email"`
	Postcode string `json:"postcode"`
}

// @Summary		Register interest in an uncovered postcode
// @Tags			storefront
// @Accept			json
// @Produce		json
// @Param			body	body	zoneCoverageRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/storefront/zone-coverage-notify [post]
func (h *Handler) ZoneCoverageNotify(w http.ResponseWriter, r *http.Request) {
	var req zoneCoverageRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.svc.ZoneCoverageNotify(r.Context(), req.Email, req.Postcode); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"success": true})
}

func derefPricing(p *franchiseapi.DeliveryPricing) franchiseapi.DeliveryPricing {
	if p == nil {
		return franchiseapi.DeliveryPricing{}
	}
	return *p
}
