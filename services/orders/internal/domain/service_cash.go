package domain

import (
	"context"
	"errors"
	"strings"
	"time"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/tenant"
	"bakehouse/pkg/timex"
	"bakehouse/services/franchise/franchiseapi"
	"bakehouse/services/orders/ordersapi"
)

/*
 * The cash drawer: the float it opens with, what goes in and out of it during
 * the day, and what is actually in it when the day is closed.
 *
 * Two rules run through all of it.
 *
 * First, only cash counts. Card and UPI takings are reported alongside because
 * the day's takings are worth seeing whole, but they never reach the drawer
 * and so can neither explain nor excuse what is missing from it.
 *
 * Second, a day is the outlet's day. A shop trading until 9pm in New Jersey is
 * still on the same date when UTC has already rolled over, and a close that
 * split the evening across two dates would be wrong in a way nobody would
 * notice until somebody audited the numbers.
 */

// GetCashDay returns one day's float, movements and running totals.
func (s *Service) GetCashDay(ctx context.Context, claims *auth.StaffClaims, branchID, date string) (*ordersapi.CashDay, []ordersapi.CashMovement, error) {
	branchID, date, err := s.resolveCashScope(ctx, claims, branchID, date)
	if err != nil {
		return nil, nil, err
	}

	day, err := s.cashDay(ctx, branchID, date)
	if err != nil {
		return nil, nil, err
	}
	movements, err := s.store.ListCashMovements(ctx, branchID, date)
	if err != nil {
		return nil, nil, apperr.Internal(err)
	}
	return day, movements, nil
}

/*
 * cashDay reads a day, falling back to the standing float.
 *
 * A day nobody has opened is not an error — it is a shop that has not started
 * trading yet. It comes back with whatever the "apply the same amount daily"
 * tick last said, flagged as inherited so the screen can say where the number
 * came from rather than implying somebody counted it this morning.
 */
func (s *Service) cashDay(ctx context.Context, branchID, date string) (*ordersapi.CashDay, error) {
	day, err := s.store.GetCashDay(ctx, branchID, date)
	if err == nil {
		return day, nil
	}
	if !errors.Is(err, ordersapi.ErrNotFound) {
		return nil, apperr.Internal(err)
	}

	standing, found, err := s.store.StandingFloat(ctx, branchID, date)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return &ordersapi.CashDay{
		BranchID:              branchID,
		BusinessDate:          date,
		StartingCashCents:     standing,
		CarryForward:          found,
		StartingCashInherited: found,
	}, nil
}

// SetStartingCash records the float the drawer opens with.
func (s *Service) SetStartingCash(ctx context.Context, claims *auth.StaffClaims, in ordersapi.StartingCashInput) (*ordersapi.CashDay, error) {
	if !tenant.HasPermission(claims, tenant.PermCashManage) {
		return nil, apperr.PermissionDenied("You do not have permission to manage the cash drawer.")
	}
	branchID, date, err := s.resolveCashScope(ctx, claims, in.BranchID, in.BusinessDate)
	if err != nil {
		return nil, err
	}
	in.BranchID, in.BusinessDate = branchID, date

	if in.AmountCents < 0 {
		return nil, apperr.Validation("A starting float cannot be negative.").
			WithFields(apperr.FieldError{Field: "amountCents", Code: "MIN", Message: "Zero or more."})
	}

	day, err := s.store.UpsertStartingCash(ctx, in)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	s.writeOrderAudit(ctx, claims, "pos.cash.starting_cash", branchID, map[string]any{
		"businessDate": date, "amountCents": in.AmountCents, "carryForward": in.CarryForward,
	})
	return day, nil
}

// AddCashMovement records a pay-in or a pay-out.
func (s *Service) AddCashMovement(ctx context.Context, claims *auth.StaffClaims, in ordersapi.CashMovementInput) (*ordersapi.CashMovement, error) {
	if !tenant.HasPermission(claims, tenant.PermCashManage) {
		return nil, apperr.PermissionDenied("You do not have permission to manage the cash drawer.")
	}
	branchID, date, err := s.resolveCashScope(ctx, claims, in.BranchID, in.BusinessDate)
	if err != nil {
		return nil, err
	}
	in.BranchID, in.BusinessDate = branchID, date

	if in.Kind != ordersapi.CashPayIn && in.Kind != ordersapi.CashPayOut {
		return nil, apperr.Validation("A movement is either a payin or a payout.").
			WithFields(apperr.FieldError{Field: "kind", Code: "INVALID", Message: "payin or payout."})
	}
	if in.AmountCents <= 0 {
		return nil, apperr.Validation("Enter an amount.").
			WithFields(apperr.FieldError{Field: "amountCents", Code: "MIN", Message: "More than zero."})
	}

	if in.Kind == ordersapi.CashPayOut {
		live, err := s.buildDayCloseSummary(ctx, branchID, date)
		if err != nil {
			return nil, err
		}
		if live.IsClosed {
			return nil, apperr.New(apperr.CodeCashDayAlreadyClosed, apperr.CategoryConflict,
				"This day is already closed. Reopen it before recording more cash movements.")
		}
		if in.AmountCents > live.ExpectedCashCents {
			return nil, apperr.New(apperr.CodeCashPayoutExceedsAvailable, apperr.CategoryValidation,
				"This payout is more than what's currently in the drawer.").
				WithFields(apperr.FieldError{
					Field: "amountCents", Code: "EXCEEDS_AVAILABLE",
					Message: "Cannot exceed the cash available in the drawer.",
				}).
				WithMeta(map[string]any{"availableCents": live.ExpectedCashCents})
		}
	}

	m, err := s.store.AddCashMovement(ctx, in, claims.StaffID)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	s.writeOrderAudit(ctx, claims, "pos.cash."+in.Kind, branchID, map[string]any{
		"businessDate": date, "amountCents": in.AmountCents, "reason": in.Reason,
	})
	return m, nil
}

// ListCashMovements returns a day's pay-ins and pay-outs.
func (s *Service) ListCashMovements(ctx context.Context, claims *auth.StaffClaims, branchID, date string) ([]ordersapi.CashMovement, error) {
	branchID, date, err := s.resolveCashScope(ctx, claims, branchID, date)
	if err != nil {
		return nil, err
	}
	out, err := s.store.ListCashMovements(ctx, branchID, date)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return out, nil
}

// DeleteCashMovement removes an entry keyed in by mistake.
func (s *Service) DeleteCashMovement(ctx context.Context, claims *auth.StaffClaims, id string) error {
	if !tenant.HasPermission(claims, tenant.PermCashManage) {
		return apperr.PermissionDenied("You do not have permission to manage the cash drawer.")
	}
	m, err := s.store.GetCashMovement(ctx, id)
	if err != nil {
		return notFoundOrInternal(err, "Entry not found.")
	}
	if _, _, err := s.resolveCashScope(ctx, claims, m.BranchID, m.BusinessDate); err != nil {
		return err
	}
	if err := s.store.DeleteCashMovement(ctx, id); err != nil {
		return notFoundOrInternal(err, "Entry not found.")
	}
	s.writeOrderAudit(ctx, claims, "pos.cash.movement_deleted", m.BranchID, map[string]any{
		"businessDate": m.BusinessDate, "kind": m.Kind, "amountCents": m.AmountCents,
	})
	return nil
}

/*
 * DayCloseSummary is the reconciliation, computed live.
 *
 * Reading it does not close anything — the screen shows these numbers while
 * the day is still running, and the person at the counter compares them with
 * what is in front of them before committing to a count.
 */
func (s *Service) DayCloseSummary(ctx context.Context, claims *auth.StaffClaims, branchID, date string) (*ordersapi.DayCloseSummary, error) {
	branchID, date, err := s.resolveCashScope(ctx, claims, branchID, date)
	if err != nil {
		return nil, err
	}
	return s.buildDayCloseSummary(ctx, branchID, date)
}

func (s *Service) buildDayCloseSummary(ctx context.Context, branchID, date string) (*ordersapi.DayCloseSummary, error) {
	day, err := s.cashDay(ctx, branchID, date)
	if err != nil {
		return nil, err
	}
	payIns, payOuts, err := s.store.CashMovementTotals(ctx, branchID, date)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	tenders, err := s.store.TenderTotals(ctx, branchID, date)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	orderCount, err := s.store.OrderCountOn(ctx, branchID, date)
	if err != nil {
		return nil, apperr.Internal(err)
	}

	var cash, nonCash, total int64
	other := []ordersapi.TenderTotal{}
	for _, t := range tenders {
		total += t.AmountCents
		if t.Method == "cash" {
			cash += t.AmountCents
			continue
		}
		nonCash += t.AmountCents
		other = append(other, t)
	}

	sum := &ordersapi.DayCloseSummary{
		BranchID:              branchID,
		BusinessDate:          date,
		StartingCashCents:     day.StartingCashCents,
		StartingCashInherited: day.StartingCashInherited,
		CashSalesCents:        cash,
		PayInsCents:           payIns,
		PayOutsCents:          payOuts,
		ExpectedCashCents:     day.StartingCashCents + cash + payIns - payOuts,
		NonCash:               other,
		NonCashCents:          nonCash,
		TotalTakingsCents:     total,
		OrderCount:            orderCount,
		ClosedAt:              day.ClosedAt,
		Note:                  day.Note,
		IsClosed:              day.ClosedAt != nil,
	}

	/*
	 * A closed day reports the figures it was closed on, not today's.
	 *
	 * Expected cash is recomputed above from live data, which is right while
	 * the day is open and wrong once it is not: a refund keyed in the next
	 * morning would otherwise rewrite last night's shortage into a number
	 * nobody signed off on.
	 */
	if day.ClosedAt != nil {
		sum.CountedCashCents = day.CountedCashCents
		sum.ShortageCents = day.ShortageCents
		if day.ExpectedCashCents != nil {
			sum.ExpectedCashCents = *day.ExpectedCashCents
		}
	}
	return sum, nil
}

/*
 * CloseDay records what was actually in the drawer.
 *
 *	shortage = counted - expected
 *
 * Signed, and deliberately not clamped: a drawer with too much in it is as
 * much of a problem as one with too little, and reporting an overage as zero
 * would hide the till error that caused it.
 *
 * Closing the same day again overwrites the record. A miscount found ten
 * minutes later should be correctable without leaving two accounts of the same
 * evening for somebody to choose between.
 */
func (s *Service) CloseDay(ctx context.Context, claims *auth.StaffClaims, in ordersapi.DayCloseInput) (*ordersapi.DayCloseSummary, error) {
	if !tenant.HasPermission(claims, tenant.PermCashManage) {
		return nil, apperr.PermissionDenied("You do not have permission to close the day.")
	}
	branchID, date, err := s.resolveCashScope(ctx, claims, in.BranchID, in.BusinessDate)
	if err != nil {
		return nil, err
	}
	in.BranchID, in.BusinessDate = branchID, date

	if in.CountedCashCents < 0 {
		return nil, apperr.Validation("A counted drawer cannot be negative.").
			WithFields(apperr.FieldError{Field: "countedCashCents", Code: "MIN", Message: "Zero or more."})
	}

	// Computed from the day as it stands right now, then stored, so the close
	// is a record of a moment rather than a view that keeps changing.
	live, err := s.buildDayCloseSummary(ctx, branchID, date)
	if err != nil {
		return nil, err
	}
	expected := live.ExpectedCashCents
	if live.IsClosed {
		// Re-closing: recompute against today's data rather than reusing the
		// figure from the first close, since the point of re-closing is that
		// something about the day has changed.
		expected = live.StartingCashCents + live.CashSalesCents + live.PayInsCents - live.PayOutsCents
	}
	shortage := in.CountedCashCents - expected

	if _, err := s.store.CloseDay(ctx, in, claims.StaffID, expected, shortage); err != nil {
		return nil, apperr.Internal(err)
	}

	s.writeOrderAudit(ctx, claims, "pos.cash.day_close", branchID, map[string]any{
		"businessDate": date, "countedCashCents": in.CountedCashCents,
		"expectedCashCents": expected, "shortageCents": shortage,
	})
	return s.buildDayCloseSummary(ctx, branchID, date)
}

/*
 * DailyReport is the history: one row per day, plus totals and best sellers.
 *
 * Every day in the range appears, including ones the shop did not trade. A
 * report that silently omits the quiet days makes a closed Monday and a
 * forgotten Monday look identical.
 */
func (s *Service) DailyReport(ctx context.Context, claims *auth.StaffClaims, branchID, from, to string) (*ordersapi.DailyReport, error) {
	if !tenant.HasPermission(claims, tenant.PermReportsOwnBranch) {
		return nil, apperr.PermissionDenied("You do not have permission to view reports.")
	}
	branchID, _, err := s.resolveCashScope(ctx, claims, branchID, "")
	if err != nil {
		return nil, err
	}

	today, err := s.store.BranchToday(ctx, branchID)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if strings.TrimSpace(to) == "" {
		to = today
	}
	if strings.TrimSpace(from) == "" {
		t, parseErr := timex.ParseDate(to)
		if parseErr != nil {
			return nil, invalidDate("to")
		}
		from = t.AddDate(0, 0, -29).Format(timex.DateLayout)
	}
	if err := validDateRange(from, to); err != nil {
		return nil, err
	}

	days, err := s.store.DailyReportRows(ctx, branchID, from, to)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	topItems, err := s.store.TopProducts(ctx, branchID, from, to, 10)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	topCategories, err := s.store.TopCategoriesForRange(ctx, branchID, from, to, 10)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	salesSummary, err := s.store.SalesSummaryForRange(ctx, branchID, from, to)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	transactionSummary, err := s.store.TransactionSummaryForRange(ctx, branchID, from, to)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	returnItems, err := s.store.ReturnItemsForRange(ctx, branchID, from, to)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	openOrders, err := s.store.OpenOrdersSummary(ctx, branchID)
	if err != nil {
		return nil, apperr.Internal(err)
	}

	// Same date picked twice = "today's chart" — break it down by hour.
	var hours []ordersapi.HourlyReportRow
	if from == to {
		hours, err = s.hourlyReportForDay(ctx, branchID, from)
		if err != nil {
			return nil, apperr.Internal(err)
		}
	}

	totals := ordersapi.DailyReportRow{BusinessDate: "total"}
	for _, d := range days {
		totals.OrderCount += d.OrderCount
		totals.GrossSalesCents += d.GrossSalesCents
		totals.DiscountCents += d.DiscountCents
		totals.TaxCents += d.TaxCents
		totals.ServiceChargeCents += d.ServiceChargeCents
		totals.NetSalesCents += d.NetSalesCents
		totals.RefundCents += d.RefundCents
		totals.CancelledOrderCount += d.CancelledOrderCount
		totals.CancelledOrderCents += d.CancelledOrderCents
		totals.ReturnedOrderCount += d.ReturnedOrderCount
		totals.ReturnedOrderCents += d.ReturnedOrderCents
		totals.DeliveryChargeCents += d.DeliveryChargeCents
		totals.CashCents += d.CashCents
		totals.CardCents += d.CardCents
		totals.OnlineCents += d.OnlineCents
		totals.OnlineCount += d.OnlineCount
	}

	return &ordersapi.DailyReport{
		BranchID: branchID, From: from, To: to,
		Days: days, Totals: totals,
		OpenOrders:         openOrders,
		Hours:              hours,
		SalesSummary:       salesSummary,
		TransactionSummary: transactionSummary,
		TopItems:           topItems,
		TopCategories:      topCategories,
		ReturnItems:        returnItems,
	}, nil
}

func (s *Service) hourlyReportForDay(ctx context.Context, branchID, date string) ([]ordersapi.HourlyReportRow, error) {
	branch, err := s.branches.GetBranch(ctx, branchID)
	if err != nil {
		return nil, err
	}
	d, err := timex.ParseDate(date)
	if err != nil {
		return nil, invalidDate("date")
	}
	exceptions, err := s.branches.ListBranchExceptions(ctx, branchID)
	if err != nil {
		return nil, err
	}
	var exception franchiseapi.BranchException
	for _, e := range exceptions {
		if e.Date.Format(timex.DateLayout) == date {
			exception = e
			break
		}
	}

	open, close, closed := resolveDayHours(branch, exception, d)
	if closed || open == "" || close == "" {
		return []ordersapi.HourlyReportRow{}, nil
	}
	openT, err := time.Parse("15:04", normaliseTime(open)[:5])
	if err != nil {
		return []ordersapi.HourlyReportRow{}, nil
	}
	closeT, err := time.Parse("15:04", normaliseTime(close)[:5])
	if err != nil {
		return []ordersapi.HourlyReportRow{}, nil
	}
	closeHour := closeT.Hour()
	if closeT.Minute() == 0 && closeHour != openT.Hour() {
		closeHour = (closeHour - 1 + 24) % 24
	}

	return s.store.HourlySalesForDay(ctx, branchID, date, openT.Hour(), closeHour)
}

// --- scoping --------------------------------------------------------------

/*
 * resolveCashScope settles which outlet and which day a request is about.
 *
 * A till signed in to one shop should not have to name it on every call, and
 * "today" means the outlet's today rather than the server's. A token covering
 * several outlets has to say which one, because guessing would post somebody
 * else's takings.
 */
func (s *Service) resolveCashScope(ctx context.Context, claims *auth.StaffClaims, branchID, date string) (string, string, error) {
	id := strings.TrimSpace(branchID)
	if id == "" {
		id = soleBranchOf(claims)
	}
	if id == "" {
		return "", "", apperr.Validation("Say which outlet.").
			WithFields(apperr.FieldError{Field: "branchId", Code: "REQUIRED", Message: "Required."})
	}

	if !tenant.CanAccessBranch(claims, id) {
		return "", "", apperr.PermissionDenied("You do not have access to this branch.")
	}

	d := strings.TrimSpace(date)
	if d == "" {
		var err error
		if d, err = s.store.BranchToday(ctx, id); err != nil {
			return "", "", apperr.Internal(err)
		}
	} else if _, err := timex.ParseDate(d); err != nil {
		return "", "", invalidDate("date")
	}
	return id, d, nil
}

func invalidDate(field string) error {
	return apperr.Validation("Dates are YYYY-MM-DD.").
		WithFields(apperr.FieldError{Field: field, Code: "INVALID", Message: "YYYY-MM-DD."})
}

func validDateRange(from, to string) error {
	f, err := timex.ParseDate(from)
	if err != nil {
		return invalidDate("from")
	}
	t, err := timex.ParseDate(to)
	if err != nil {
		return invalidDate("to")
	}
	if t.Before(f) {
		return apperr.Validation("The end of the range is before its start.").
			WithFields(apperr.FieldError{Field: "to", Code: "RANGE", Message: "On or after from."})
	}
	// A year at a time. Wider than any screen shows, and narrow enough that a
	// mistyped year cannot ask the database for a decade of rows.
	if t.Sub(f) > 366*24*time.Hour {
		return apperr.Validation("Ask for a year at a time or less.").
			WithFields(apperr.FieldError{Field: "from", Code: "RANGE", Message: "At most 366 days."})
	}
	return nil
}

// soleBranchOf returns the branch a token is confined to, or "" when it
// covers none or several.
func soleBranchOf(claims *auth.StaffClaims) string {
	if claims == nil {
		return ""
	}
	found := ""
	for _, ra := range claims.RoleAssignments {
		if ra.ScopeType != tenant.ScopeBranch || ra.BranchID == nil || *ra.BranchID == "" {
			continue
		}
		if found != "" && found != *ra.BranchID {
			return ""
		}
		found = *ra.BranchID
	}
	return found
}
