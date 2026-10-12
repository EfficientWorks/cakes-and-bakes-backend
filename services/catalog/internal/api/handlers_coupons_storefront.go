package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/httpx"
	"bakehouse/services/catalog/internal/domain"
	"bakehouse/services/catalog/internal/store"
)

// storefrontCouponDTOType is a coupon as the shop shows it: enough to
// advertise the code and say what it is worth, none of the admin bookkeeping.
type storefrontCouponDTOType struct {
	ID   string `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
	// Type is the discount rule: percentage, fixed, free_delivery or free_shipping;
	// AppliesTo is all, products, categories or new_customer.
	Type          string `json:"type"`
	DiscountValue int64  `json:"discountValue"`
	AppliesTo     string `json:"appliesTo"`
	// BranchID is the branch the code is local to; null for network-wide.
	BranchID       *string `json:"branchId"`
	ShowOnCheckout bool    `json:"showOnCheckout"`
	IsNewCustomer  bool    `json:"isNewCustomer"`
	// The window, as full timestamps. Both null when the coupon has no
	// window (always the case for new_customer).
	HasActiveDates    bool    `json:"hasActiveDates"`
	StartsAt          *string `json:"startsAt"`
	EndsAt            *string `json:"endsAt"`
	HasMinimumOrder   bool    `json:"hasMinimumOrder"`
	MinimumOrderCents int64   `json:"minimumOrderCents"`
	// Eligible is whether the caller could use the code, judged without a
	// basket; IneligibleReason is "login_required" or "not_new_customer".
	Eligible         bool   `json:"eligible"`
	IneligibleReason string `json:"ineligibleReason,omitempty"`
}

func storefrontCouponDTO(c *domain.StorefrontCoupon) storefrontCouponDTOType {
	return storefrontCouponDTOType{
		ID: c.ID, Code: c.Code, Name: c.Name, Type: c.Type,
		DiscountValue: c.DiscountValue, AppliesTo: c.AppliesTo,
		BranchID: c.BranchID, ShowOnCheckout: c.ShowOnCheckout, IsNewCustomer: c.IsNewCustomer(),
		HasActiveDates: c.HasActiveDates,
		StartsAt:       formatTimePtr(c.StartsAt), EndsAt: formatTimePtr(c.EndsAt),
		HasMinimumOrder: c.HasMinimumOrder, MinimumOrderCents: c.MinimumOrderCents,
		Eligible: c.Eligible, IneligibleReason: c.IneligibleReason,
	}
}

func formatTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(rfc3339)
	return &s
}

// @Summary		List discount codes the storefront may show
// @Description	Active codes only. Filters: branchId keeps network-wide codes plus that branch's own; showOnCheckout=true keeps codes flagged for the checkout page; newCustomer=true/false keeps only first-order codes / everything else; date (YYYY-MM-DD) keeps codes whose window covers that day (codes with no window always match). With a customer bearer token, each new-customer code says whether that customer can still use it (eligible / ineligibleReason).
// @Tags			storefront
// @Produce		json
// @Param			branchId		query	string	false	"Branch id"
// @Param			showOnCheckout	query	bool	false	"Only codes flagged for the checkout page"
// @Param			newCustomer		query	bool	false	"true: only new-customer codes; false: exclude them"
// @Param			date			query	string	false	"YYYY-MM-DD; keep codes valid on that day"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/storefront/coupons [get]
func (h *Handler) ListStorefrontCoupons(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.StorefrontCouponFilter{BranchID: strings.TrimSpace(q.Get("branchId"))}
	var err error
	if f.ShowOnCheckout, err = optionalBool(q.Get("showOnCheckout"), "showOnCheckout"); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if f.NewCustomer, err = optionalBool(q.Get("newCustomer"), "newCustomer"); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if raw := strings.TrimSpace(q.Get("date")); raw != "" {
		d, perr := time.Parse("2006-01-02", raw)
		if perr != nil {
			httpx.WriteError(w, r, apperr.Validation("date must be YYYY-MM-DD.").
				WithFields(apperr.FieldError{Field: "date", Code: "INVALID", Message: "YYYY-MM-DD"}))
			return
		}
		f.Date = &d
	}

	var customerID *string
	if claims, ok := auth.CustomerFromContext(r.Context()); ok {
		customerID = &claims.CustomerID
	}

	coupons, err := h.svc.ListStorefrontCoupons(r.Context(), f, customerID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out := make([]storefrontCouponDTOType, 0, len(coupons))
	for i := range coupons {
		out = append(out, storefrontCouponDTO(&coupons[i]))
	}
	httpx.OK(w, out)
}

func optionalBool(raw, field string) (*bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return nil, apperr.Validation(field + " must be true or false.").
			WithFields(apperr.FieldError{Field: field, Code: "INVALID", Message: "true or false"})
	}
	return &v, nil
}
