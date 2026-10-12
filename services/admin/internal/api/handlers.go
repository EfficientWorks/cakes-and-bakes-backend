package api

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/httpx"
	"bakehouse/services/admin/adminapi"
	"bakehouse/services/admin/internal/domain"
)

type Handler struct {
	svc *domain.Service
}

func NewHandler(svc *domain.Service) *Handler { return &Handler{svc: svc} }

func staffClaims(r *http.Request) *auth.StaffClaims {
	c, _ := auth.StaffFromContext(r.Context())
	return c
}

// reportBranchID reads the optional branch filter on the network reports. The
// console calls a branch a "store" in places, so both names are accepted, as
// is the X-Branch-ID header the rest of the admin API honours.
func reportBranchID(r *http.Request) string {
	q := r.URL.Query()
	for _, v := range []string{q.Get("branchId"), q.Get("storeId"), r.Header.Get("X-Branch-ID"), r.Header.Get("X-Store-ID")} {
		if v != "" {
			return v
		}
	}
	return ""
}

// dateRange reads from/to query params (YYYY-MM-DD, half-open [from,to)),
// defaulting to the trailing 30 days ending tomorrow (so "to" is inclusive
// of today) when omitted.
func dateRange(r *http.Request) (time.Time, time.Time, error) {
	q := r.URL.Query()
	to := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, 1)
	from := to.AddDate(0, 0, -30)
	if v := q.Get("from"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			return time.Time{}, time.Time{}, apperr.Validation("from must be YYYY-MM-DD.")
		}
		from = t
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			return time.Time{}, time.Time{}, apperr.Validation("to must be YYYY-MM-DD.")
		}
		to = t.AddDate(0, 0, 1) // "to" is the last included day, so the exclusive bound is the day after
	}
	if !from.Before(to) {
		return time.Time{}, time.Time{}, apperr.Validation("from must be before to.")
	}
	return from, to, nil
}

func wantsCSV(r *http.Request) bool {
	return r.URL.Query().Get("format") == "csv"
}

func writeCSV(w http.ResponseWriter, filename string, header []string, rows [][]string) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	cw := csv.NewWriter(w)
	_ = cw.Write(header)
	for _, row := range rows {
		_ = cw.Write(row)
	}
	cw.Flush()
}

// --- Branch dashboard & reports (ADM-20, ADM-21) ----------------------------

// @Summary		Branch dashboard (today's orders, revenue, slot load)
// @Tags			admin-reports
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	path	string	true	"branchId"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/branches/{branchId}/dashboard [get]
// @Summary		Today's orders and takings for every outlet in scope
// @Tags			admin-reports
// @Produce		json
// @Security		BearerAuth
// @Success		200	{object}	httpx.Envelope
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/admin/branches/today [get]
func (h *Handler) OutletsToday(w http.ResponseWriter, r *http.Request) {
	rows, err := h.svc.OutletsToday(r.Context(), staffClaims(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, rows)
}

func (h *Handler) BranchDashboard(w http.ResponseWriter, r *http.Request) {
	branchID := chi.URLParam(r, "branchId")
	d, err := h.svc.BranchDashboard(r.Context(), staffClaims(r), branchID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, d)
}

// @Summary		Branch sales by day
// @Tags			admin-reports
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	path	string	true	"branchId"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/branches/{branchId}/reports/sales [get]
func (h *Handler) BranchSales(w http.ResponseWriter, r *http.Request) {
	branchID := chi.URLParam(r, "branchId")
	from, to, err := dateRange(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	points, err := h.svc.SalesByDay(r.Context(), staffClaims(r), branchID, from, to)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if wantsCSV(r) {
		rows := make([][]string, 0, len(points))
		for _, p := range points {
			rows = append(rows, []string{p.Date, strconv.Itoa(p.OrderCount), strconv.FormatInt(p.RevenueCents, 10)})
		}
		writeCSV(w, "branch-sales.csv", []string{"date", "orderCount", "revenueCents"}, rows)
		return
	}
	httpx.OK(w, points)
}

// @Summary		Branch product performance
// @Tags			admin-reports
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	path	string	true	"branchId"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/branches/{branchId}/reports/products [get]
func (h *Handler) BranchProducts(w http.ResponseWriter, r *http.Request) {
	branchID := chi.URLParam(r, "branchId")
	from, to, err := dateRange(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	list, err := h.svc.BranchProductPerformance(r.Context(), staffClaims(r), branchID, from, to)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	writeProductPerformance(w, r, list)
}

func writeProductPerformance(w http.ResponseWriter, r *http.Request, list []adminapi.ProductPerformance) {
	if wantsCSV(r) {
		rows := make([][]string, 0, len(list))
		for _, p := range list {
			rows = append(rows, []string{p.ProductID, p.ProductName, strconv.Itoa(p.QuantitySold), strconv.FormatInt(p.RevenueCents, 10)})
		}
		writeCSV(w, "product-performance.csv", []string{"productId", "productName", "quantitySold", "revenueCents"}, rows)
		return
	}
	best := list
	var worst []adminapi.ProductPerformance
	const topN = 10
	if len(list) > topN {
		best = list[:topN]
	}
	if len(list) > 0 {
		start := len(list) - topN
		if start < 0 {
			start = 0
		}
		worst = list[start:]
	}
	httpx.OK(w, map[string]interface{}{"bestSellers": best, "worstSellers": worst, "all": list})
}

// @Summary		Branch fulfilment performance (prep time, on-time rate)
// @Tags			admin-reports
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	path	string	true	"branchId"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/branches/{branchId}/reports/fulfilment [get]
func (h *Handler) BranchFulfilment(w http.ResponseWriter, r *http.Request) {
	branchID := chi.URLParam(r, "branchId")
	from, to, err := dateRange(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	f, err := h.svc.BranchFulfilmentPerformance(r.Context(), staffClaims(r), branchID, from, to)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, f)
}

// @Summary		Branch refunds and cancellations
// @Tags			admin-reports
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	path	string	true	"branchId"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/branches/{branchId}/reports/refunds [get]
func (h *Handler) BranchRefunds(w http.ResponseWriter, r *http.Request) {
	branchID := chi.URLParam(r, "branchId")
	from, to, err := dateRange(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	stats, err := h.svc.BranchRefundStats(r.Context(), staffClaims(r), branchID, from, to)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, stats)
}

// @Summary		Branch order-amendment log
// @Tags			admin-reports
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	path	string	true	"branchId"
// @Param			page	query	int	false	"Page number (default 1)"
// @Param			limit	query	int	false	"Page size (default 50, max 200)"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/branches/{branchId}/amendments [get]
func (h *Handler) BranchAmendments(w http.ResponseWriter, r *http.Request) {
	branchID := chi.URLParam(r, "branchId")
	from, to, err := dateRange(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))

	list, total, err := h.svc.BranchAmendmentLog(r.Context(), staffClaims(r), branchID, from, to, page, limit)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OKWithMeta(w, list, map[string]int{"page": maxInt(page, 1), "total": total})
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// --- Network dashboard & reports (REP-01 to REP-14) -------------------------

// @Summary		Network dashboard across every branch
// @Description	Today's totals, the live pipeline (confirmed, scheduled, in production, ready, out for delivery) and the branch league. branchId narrows every figure. Callers without network reporting are pinned to their own branch.
// @Tags			admin-network
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	query	string	false	"Restrict to one branch (storeId also accepted)"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/admin/network/dashboard [get]
func (h *Handler) NetworkDashboard(w http.ResponseWriter, r *http.Request) {
	d, err := h.svc.NetworkDashboard(r.Context(), staffClaims(r), reportBranchID(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, d)
}

// @Summary		Revenue trend by day, week or month — all branches or one
// @Description	Returns scope, overall totals (orders, revenue, average order) for the period, zero-filled points per period, and a per-branch breakdown with each branch's share. branchId omitted or "all" = every branch. Branch-scoped callers are pinned to their own branch.
// @Tags			admin-network
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	query	string	false	"A branch id, or 'all' (default). storeId also accepted"
// @Param			unit		query	string	false	"day (default), week or month"
// @Param			from		query	string	false	"YYYY-MM-DD, default 30 days ago"
// @Param			to			query	string	false	"YYYY-MM-DD inclusive, default today"
// @Param			format		query	string	false	"csv for a download"
// @Success		200	{object}	httpx.Envelope{data=adminapi.RevenueTrend}
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/admin/network/revenue-trend [get]
func (h *Handler) NetworkRevenueTrend(w http.ResponseWriter, r *http.Request) {
	from, to, err := dateRange(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	q := r.URL.Query()
	trend, err := h.svc.RevenueTrend(r.Context(), staffClaims(r), reportBranchID(r), q.Get("unit"), from, to)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if wantsCSV(r) {
		rows := make([][]string, 0, len(trend.Points)+1)
		for _, p := range trend.Points {
			rows = append(rows, []string{p.Period, strconv.Itoa(p.OrderCount), strconv.FormatInt(p.RevenueCents, 10)})
		}
		rows = append(rows, []string{"TOTAL", strconv.Itoa(trend.Totals.OrderCount), strconv.FormatInt(trend.Totals.RevenueCents, 10)})
		writeCSV(w, "revenue-trend.csv", []string{"period", "orderCount", "revenueCents"}, rows)
		return
	}
	httpx.OK(w, trend)
}

// @Summary		Network product performance
// @Tags			admin-network
// @Produce		json
// @Security		BearerAuth
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/admin/network/products [get]
func (h *Handler) NetworkProducts(w http.ResponseWriter, r *http.Request) {
	from, to, err := dateRange(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	list, err := h.svc.NetworkProductPerformance(r.Context(), staffClaims(r), from, to)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	writeProductPerformance(w, r, list)
}

// @Summary		Each outlet's trading over a period
// @Description	Orders, revenue and average order value per outlet for the chosen window — what the reporting screen's outlet comparison is built from. Excludes cancelled orders.
// @Tags			admin-network
// @Produce		json
// @Security		BearerAuth
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/admin/network/branch-performance [get]
func (h *Handler) NetworkBranchPerformance(w http.ResponseWriter, r *http.Request) {
	from, to, err := dateRange(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	list, err := h.svc.NetworkBranchPerformance(r.Context(), staffClaims(r), from, to)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, list)
}

// @Summary		Network orders split by how they were handed over
// @Description	Pickup, delivery and takeaway, every type present even at zero so a chart's categories do not move about between days.
// @Tags			admin-network
// @Produce		json
// @Security		BearerAuth
// @Param			from	query	string	false	"Start date, YYYY-MM-DD"
// @Param			to	query	string	false	"End date, YYYY-MM-DD"
// @Param			branchId	query	string	false	"Restrict to one branch (storeId also accepted)"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/admin/network/fulfilment [get]
func (h *Handler) NetworkFulfilment(w http.ResponseWriter, r *http.Request) {
	from, to, err := dateRange(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	list, err := h.svc.NetworkFulfilmentBreakdown(r.Context(), staffClaims(r), reportBranchID(r), from, to)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, list)
}

// @Summary		Network revenue split by channel
// @Tags			admin-network
// @Produce		json
// @Security		BearerAuth
// @Param			from	query	string	false	"Start date, YYYY-MM-DD"
// @Param			to	query	string	false	"End date, YYYY-MM-DD"
// @Param			branchId	query	string	false	"Restrict to one branch (storeId also accepted)"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/admin/network/channels [get]
func (h *Handler) NetworkChannels(w http.ResponseWriter, r *http.Request) {
	from, to, err := dateRange(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	list, err := h.svc.NetworkChannelBreakdown(r.Context(), staffClaims(r), reportBranchID(r), from, to)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if wantsCSV(r) {
		rows := make([][]string, 0, len(list))
		for _, c := range list {
			rows = append(rows, []string{c.Channel, strconv.Itoa(c.OrderCount), strconv.FormatInt(c.RevenueCents, 10)})
		}
		writeCSV(w, "channel-breakdown.csv", []string{"channel", "orderCount", "revenueCents"}, rows)
		return
	}
	httpx.OK(w, list)
}

// @Summary		Network order volume by day and hour
// @Tags			admin-network
// @Produce		json
// @Security		BearerAuth
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/admin/network/peak-periods [get]
func (h *Handler) NetworkPeakPeriods(w http.ResponseWriter, r *http.Request) {
	from, to, err := dateRange(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	list, err := h.svc.NetworkPeakPeriods(r.Context(), staffClaims(r), from, to)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, list)
}

// @Summary		Network refunds and cancellations by branch
// @Tags			admin-network
// @Produce		json
// @Security		BearerAuth
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/admin/network/refunds [get]
func (h *Handler) NetworkRefunds(w http.ResponseWriter, r *http.Request) {
	from, to, err := dateRange(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	list, err := h.svc.NetworkRefundStats(r.Context(), staffClaims(r), from, to)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if wantsCSV(r) {
		rows := make([][]string, 0, len(list))
		for _, s := range list {
			rows = append(rows, []string{
				s.BranchID, s.BranchName, strconv.Itoa(s.TotalOrders), strconv.Itoa(s.CancelledOrders),
				fmt.Sprintf("%.2f", s.CancellationPct), strconv.Itoa(s.RefundedOrders), strconv.FormatInt(s.RefundedCents, 10),
				fmt.Sprintf("%.2f", s.RefundRatePct),
			})
		}
		writeCSV(w, "refund-cancellation-rates.csv",
			[]string{"branchId", "branchName", "totalOrders", "cancelledOrders", "cancellationRatePct", "refundedOrders", "refundedCents", "refundRatePct"}, rows)
		return
	}
	httpx.OK(w, list)
}

// @Summary		Like-for-like sales against a comparison period
// @Tags			admin-network
// @Produce		json
// @Security		BearerAuth
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/admin/network/comparable-sales [get]
func (h *Handler) NetworkComparableSales(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	currentFrom, currentTo, err := parseRequiredRange(q, "current")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	compareFrom, compareTo, err := parseRequiredRange(q, "compare")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	maturityMonths, _ := strconv.Atoi(q.Get("maturityMonths"))

	report, err := h.svc.ComparableSales(r.Context(), staffClaims(r), adminapi.ComparableSalesFilter{
		CurrentFrom: currentFrom, CurrentTo: currentTo,
		CompareFrom: compareFrom, CompareTo: compareTo,
		MaturityMonths: maturityMonths,
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if wantsCSV(r) {
		rows := make([][]string, 0, len(report.Comparable))
		for _, c := range report.Comparable {
			rows = append(rows, []string{
				c.BranchID, c.BranchName,
				strconv.FormatInt(c.CurrentRevenueCents, 10), strconv.Itoa(c.CurrentOrders),
				strconv.FormatInt(c.CompareRevenueCents, 10), strconv.Itoa(c.CompareOrders),
				fmt.Sprintf("%.2f", c.RevenueGrowthPct), fmt.Sprintf("%.2f", c.OrderGrowthPct),
			})
		}
		writeCSV(w, "comparable-sales.csv",
			[]string{"branchId", "branchName", "currentRevenueCents", "currentOrders", "compareRevenueCents", "compareOrders", "revenueGrowthPct", "orderGrowthPct"}, rows)
		return
	}
	httpx.OK(w, report)
}

// parseRequiredRange reads "<prefix>From"/"<prefix>To" (YYYY-MM-DD,
// inclusive) query params — used by comparable-sales, which needs two
// independent date ranges rather than the single dateRange() default.
func parseRequiredRange(q map[string][]string, prefix string) (time.Time, time.Time, error) {
	get := func(key string) string {
		if v, ok := q[key]; ok && len(v) > 0 {
			return v[0]
		}
		return ""
	}
	fromStr, toStr := get(prefix+"From"), get(prefix+"To")
	if fromStr == "" || toStr == "" {
		return time.Time{}, time.Time{}, apperr.Validation(prefix + "From and " + prefix + "To are required (YYYY-MM-DD).")
	}
	from, err := time.Parse("2006-01-02", fromStr)
	if err != nil {
		return time.Time{}, time.Time{}, apperr.Validation(prefix + "From must be YYYY-MM-DD.")
	}
	to, err := time.Parse("2006-01-02", toStr)
	if err != nil {
		return time.Time{}, time.Time{}, apperr.Validation(prefix + "To must be YYYY-MM-DD.")
	}
	return from, to.AddDate(0, 0, 1), nil
}

// @Summary		Search the audit log
// @Tags			admin-network
// @Produce		json
// @Security		BearerAuth
// @Param			page	query	int	false	"Page number (default 1)"
// @Param			limit	query	int	false	"Page size (default 50, max 200)"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/admin/network/audit-log [get]
func (h *Handler) NetworkAuditLog(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))

	f := adminapi.AuditSearchFilter{
		ActorID: q.Get("actorId"), ActorType: q.Get("actorType"), Action: q.Get("action"),
		ResourceType: q.Get("resourceType"), BranchID: q.Get("branchId"),
		Page: page, Limit: limit,
	}
	if v := q.Get("dateFrom"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			f.DateFrom = &t
		}
	}
	if v := q.Get("dateTo"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			t = t.AddDate(0, 0, 1)
			f.DateTo = &t
		}
	}

	list, total, err := h.svc.SearchAuditLog(r.Context(), staffClaims(r), f)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OKWithMeta(w, list, map[string]int{"page": maxInt(page, 1), "total": total})
}

// --- Customer record management (ADM-19) ------------------------------------

// @Summary		Search and list customers
// @Tags			admin-customers
// @Produce		json
// @Security		BearerAuth
// @Param			page	query	int	false	"Page number (default 1)"
// @Param			limit	query	int	false	"Page size (default 50, max 200)"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/admin/customers [get]
func (h *Handler) ListCustomers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))

	list, total, err := h.svc.ListCustomers(r.Context(), staffClaims(r), adminapi.CustomerListFilter{
		Search: q.Get("search"), BranchID: q.Get("branchId"),
		Source: q.Get("source"),
		Page:   page, Limit: limit,
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OKWithMeta(w, list, map[string]int{"page": maxInt(page, 1), "total": total})
}

// @Summary		Get one customer record
// @Tags			admin-customers
// @Produce		json
// @Security		BearerAuth
// @Param			customerId	path	string	true	"customerId"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/customers/{customerId} [get]
func (h *Handler) GetCustomer(w http.ResponseWriter, r *http.Request) {
	c, err := h.svc.GetCustomer(r.Context(), staffClaims(r), chi.URLParam(r, "customerId"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, c)
}

// @Summary		List a customer's orders
// @Tags			admin-customers
// @Produce		json
// @Security		BearerAuth
// @Param			customerId	path	string	true	"customerId"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/customers/{customerId}/orders [get]
func (h *Handler) GetCustomerOrders(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.GetCustomerOrders(r.Context(), staffClaims(r), chi.URLParam(r, "customerId"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, list)
}

// @Summary		List notes on a customer
// @Tags			admin-customers
// @Produce		json
// @Security		BearerAuth
// @Param			customerId	path	string	true	"customerId"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/customers/{customerId}/notes [get]
func (h *Handler) ListCustomerNotes(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.GetCustomerNotes(r.Context(), staffClaims(r), chi.URLParam(r, "customerId"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, list)
}

type addNoteRequest struct {
	Note string `json:"note"`
}

// @Summary		Add a note to a customer
// @Tags			admin-customers
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			customerId	path	string	true	"customerId"
// @Param			body	body	addNoteRequest	true	"Request body"
// @Success		201	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/customers/{customerId}/notes [post]
func (h *Handler) AddCustomerNote(w http.ResponseWriter, r *http.Request) {
	var req addNoteRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	n, err := h.svc.AddCustomerNote(r.Context(), staffClaims(r), chi.URLParam(r, "customerId"), req.Note)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Created(w, n)
}

type addTagRequest struct {
	Tag string `json:"tag"`
}

// @Summary		Tag a customer
// @Tags			admin-customers
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			customerId	path	string	true	"customerId"
// @Param			body	body	addTagRequest	true	"Request body"
// @Success		201	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/customers/{customerId}/tags [post]
func (h *Handler) AddCustomerTag(w http.ResponseWriter, r *http.Request) {
	var req addTagRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.svc.AddCustomerTag(r.Context(), staffClaims(r), chi.URLParam(r, "customerId"), req.Tag); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Created(w, map[string]string{"tag": req.Tag})
}

// @Summary		Remove a customer tag
// @Tags			admin-customers
// @Produce		json
// @Security		BearerAuth
// @Param			customerId	path	string	true	"customerId"
// @Param			tag	path	string	true	"tag"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/customers/{customerId}/tags/{tag} [delete]
func (h *Handler) RemoveCustomerTag(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.RemoveCustomerTag(r.Context(), staffClaims(r), chi.URLParam(r, "customerId"), chi.URLParam(r, "tag")); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"success": true})
}
