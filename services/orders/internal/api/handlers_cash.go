package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/httpx"
	"bakehouse/services/orders/ordersapi"
)

// startingCashRequest sets the float the drawer opens with.
type startingCashRequest struct {
	BranchID string `json:"branchId,omitempty"`
	// YYYY-MM-DD. Defaults to the outlet's today.
	BusinessDate string `json:"businessDate,omitempty"`
	AmountCents  int64  `json:"amountCents"`
	// The "apply the same amount daily" tick: later days nobody has opened
	// inherit this float instead of starting at zero.
	CarryForward bool `json:"carryForward,omitempty"`
}

// cashMovementRequest records money into or out of the drawer.
type cashMovementRequest struct {
	BranchID     string `json:"branchId,omitempty"`
	BusinessDate string `json:"businessDate,omitempty"`
	// payin or payout.
	Kind        string `json:"kind"`
	AmountCents int64  `json:"amountCents"`
	Reason      string `json:"reason,omitempty"`
}

// dayCloseRequest is the counted drawer.
type dayCloseRequest struct {
	BranchID         string  `json:"branchId,omitempty"`
	BusinessDate     string  `json:"businessDate,omitempty"`
	CountedCashCents int64   `json:"countedCashCents"`
	Note             string  `json:"note,omitempty"`
	ClosedAt         *string `json:"closedAt,omitempty" example:"2026-09-16T23:58:00Z"`
}

// @Summary		The cash drawer for one day
// @Description	The opening float and every pay-in and pay-out against it. A day nobody has opened comes back with the standing float, flagged as inherited.
// @Tags			pos
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	query		string	false	"Outlet (defaults to the token's own)"
// @Param			date		query		string	false	"YYYY-MM-DD (defaults to the outlet's today)"
// @Success		200			{object}	httpx.Envelope
// @Failure		400			{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		403			{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/pos/cash/day [get]
func (h *Handler) GetCashDay(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	day, movements, err := h.svc.GetCashDay(r.Context(), staffClaims(r), q.Get("branchId"), q.Get("date"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]any{"day": day, "movements": movements})
}

// @Summary		Set the opening float
// @Description	Tick carryForward for a shop that floats the same amount every morning — later days inherit it rather than opening at zero.
// @Tags			pos
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body		startingCashRequest	true	"Request body"
// @Success		200		{object}	httpx.Envelope
// @Failure		400		{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		403		{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/pos/cash/starting-cash [put]
func (h *Handler) SetStartingCash(w http.ResponseWriter, r *http.Request) {
	var req startingCashRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	day, err := h.svc.SetStartingCash(r.Context(), staffClaims(r), ordersapi.StartingCashInput{
		BranchID: req.BranchID, BusinessDate: req.BusinessDate,
		AmountCents: req.AmountCents, CarryForward: req.CarryForward,
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, day)
}

// @Summary		Record a pay-in or pay-out
// @Description	Money into or out of the drawer that is not a sale — change added, a supplier paid in cash, petty cash taken out.
// @Tags			pos
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body		cashMovementRequest	true	"Request body"
// @Success		201		{object}	httpx.Envelope
// @Failure		400		{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		403		{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/pos/cash/movements [post]
func (h *Handler) AddCashMovement(w http.ResponseWriter, r *http.Request) {
	var req cashMovementRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	m, err := h.svc.AddCashMovement(r.Context(), staffClaims(r), ordersapi.CashMovementInput{
		BranchID: req.BranchID, BusinessDate: req.BusinessDate,
		Kind: req.Kind, AmountCents: req.AmountCents, Reason: req.Reason,
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Created(w, m)
}

// @Summary		List a day's pay-ins and pay-outs
// @Tags			pos
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	query		string	false	"Outlet (defaults to the token's own)"
// @Param			date		query		string	false	"YYYY-MM-DD (defaults to the outlet's today)"
// @Success		200			{object}	httpx.Envelope
// @Failure		403			{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/pos/cash/movements [get]
func (h *Handler) ListCashMovements(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	out, err := h.svc.ListCashMovements(r.Context(), staffClaims(r), q.Get("branchId"), q.Get("date"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, out)
}

// @Summary		Remove a pay-in or pay-out
// @Description	For an entry keyed in by mistake. The day's totals recompute from what is left.
// @Tags			pos
// @Produce		json
// @Security		BearerAuth
// @Param			movementId	path		string	true	"movementId"
// @Success		200			{object}	httpx.Envelope
// @Failure		403			{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404			{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/pos/cash/movements/{movementId} [delete]
func (h *Handler) DeleteCashMovement(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "movementId")
	if err := h.svc.DeleteCashMovement(r.Context(), staffClaims(r), id); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]any{"deleted": true, "id": id})
}

// @Summary		The day-close reconciliation
// @Description	Consolidated drawer figures. expected = startingCash + cashSales + payIns − payOuts. Card and UPI are reported alongside but never reach the drawer. Reading this closes nothing.
// @Tags			pos
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	query		string	false	"Outlet (defaults to the token's own)"
// @Param			date		query		string	false	"YYYY-MM-DD (defaults to the outlet's today)"
// @Success		200			{object}	httpx.Envelope
// @Failure		400			{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		403			{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/pos/day-close [get]
func (h *Handler) GetDayClose(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sum, err := h.svc.DayCloseSummary(r.Context(), staffClaims(r), q.Get("branchId"), q.Get("date"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, sum)
}

// @Summary		Close the day
// @Description	Records what was counted in the drawer. shortage = counted − expected, signed: a negative figure is money missing, a positive one is an overage. Closing the same day again replaces the record.
// @Tags			pos
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body		dayCloseRequest	true	"Request body"
// @Success		200		{object}	httpx.Envelope
// @Failure		400		{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		403		{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/pos/day-close [post]
func (h *Handler) CloseDay(w http.ResponseWriter, r *http.Request) {
	var req dayCloseRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	r, err := httpx.WithEventTime(r, "closedAt", req.ClosedAt)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	sum, err := h.svc.CloseDay(r.Context(), staffClaims(r), ordersapi.DayCloseInput{
		BranchID: req.BranchID, BusinessDate: req.BusinessDate,
		CountedCashCents: req.CountedCashCents, Note: req.Note,
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, sum)
}

// @Summary		The daily history report
// @Description	One row per day in the range — orders, takings by tender, refunds and any shortage — plus totals and the best sellers. Days the shop did not trade are included as zeroes, so a quiet Monday and a missing Monday do not look alike.
// @Tags			pos
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	query		string	false	"Outlet (defaults to the token's own)"
// @Param			from		query		string	false	"YYYY-MM-DD (defaults to 29 days before to)"
// @Param			to			query		string	false	"YYYY-MM-DD (defaults to the outlet's today)"
// @Success		200			{object}	httpx.Envelope
// @Failure		400			{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		403			{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/pos/reports/daily [get]
func (h *Handler) DailyReport(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rep, err := h.svc.DailyReport(r.Context(), staffClaims(r), q.Get("branchId"), q.Get("from"), q.Get("to"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, rep)
}
