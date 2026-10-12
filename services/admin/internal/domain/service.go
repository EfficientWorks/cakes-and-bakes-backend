package domain

import (
	"bakehouse/pkg/email"
	"errors"
	"math"
	"strings"
	"time"

	"context"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/tenant"
	"bakehouse/services/admin/adminapi"
	"bakehouse/services/admin/internal/store"
)

// Service enforces scope (which branch a caller may see) and
// permission (which reports a role may run) on top of store.Store's direct SQL.
// Every method takes the caller's staff claims and is the only place that
// decides what they're allowed to see — store.Store has no notion of RBAC.
type Service struct {
	store *store.Store
	// Optional. Nil sends nothing rather than failing — an invoice that was
	// raised but not delivered is a recoverable state; one that was refused
	// because the mail provider is down is not.
	emailer email.Emailer
}

func NewService(store *store.Store) *Service { return &Service{store: store} }

// UseEmailer lets the admin service send invoices.
func (s *Service) UseEmailer(e email.Emailer) { s.emailer = e }

func today() time.Time {
	return time.Now().UTC().Truncate(24 * time.Hour)
}

// OutletsToday backs the two live columns on the branch directory.
//
// Scoped to whatever the caller holds, so branch staff see their own outlet
// without having to ask for it — the same rule the order list follows.
func (s *Service) OutletsToday(ctx context.Context, claims *auth.StaffClaims) ([]adminapi.BranchLeagueRow, error) {
	if !tenant.HasPermission(claims, tenant.PermReportsOwnBranch) {
		return nil, apperr.PermissionDenied("You do not have permission to view outlet figures.")
	}
	var branchID string
	if !tenant.IsBrandScoped(claims) {
		_, branchID = tenant.OwnScope(claims)
		if branchID == "" {
			return nil, apperr.PermissionDenied("You do not have a branch to report on.")
		}
	}
	rows, err := s.store.OutletsToday(ctx, today(), branchID)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return rows, nil
}

// --- Branch dashboard & reports (ADM-20, ADM-21) ----------------------------

func (s *Service) requireBranchAccess(ctx context.Context, claims *auth.StaffClaims, branchID string) (*adminapi.BranchInfo, error) {
	b, err := s.store.BranchInfoByID(ctx, branchID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, apperr.NotFound("Branch not found.")
		}
		return nil, apperr.Internal(err)
	}
	if !tenant.CanAccessBranch(claims, b.ID) {
		return nil, apperr.PermissionDenied("You do not have access to this branch.")
	}
	return b, nil
}

func (s *Service) BranchDashboard(ctx context.Context, claims *auth.StaffClaims, branchID string) (*adminapi.BranchDashboard, error) {
	if _, err := s.requireBranchAccess(ctx, claims, branchID); err != nil {
		return nil, err
	}
	d, err := s.store.BranchDashboard(ctx, branchID, today())
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return d, nil
}

func (s *Service) SalesByDay(ctx context.Context, claims *auth.StaffClaims, branchID string, from, to time.Time) ([]adminapi.SalesPoint, error) {
	if _, err := s.requireBranchAccess(ctx, claims, branchID); err != nil {
		return nil, err
	}
	out, err := s.store.SalesByDay(ctx, branchID, from, to)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return out, nil
}

func (s *Service) BranchProductPerformance(ctx context.Context, claims *auth.StaffClaims, branchID string, from, to time.Time) ([]adminapi.ProductPerformance, error) {
	if _, err := s.requireBranchAccess(ctx, claims, branchID); err != nil {
		return nil, err
	}
	out, err := s.store.ProductPerformance(ctx, branchID, from, to)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return out, nil
}

func (s *Service) BranchFulfilmentPerformance(ctx context.Context, claims *auth.StaffClaims, branchID string, from, to time.Time) (*adminapi.FulfilmentPerformance, error) {
	if _, err := s.requireBranchAccess(ctx, claims, branchID); err != nil {
		return nil, err
	}
	f, err := s.store.FulfilmentPerformance(ctx, branchID, from, to)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return f, nil
}

func (s *Service) BranchRefundStats(ctx context.Context, claims *auth.StaffClaims, branchID string, from, to time.Time) (*adminapi.RefundCancellationStats, error) {
	if _, err := s.requireBranchAccess(ctx, claims, branchID); err != nil {
		return nil, err
	}
	rows, err := s.store.RefundCancellationStats(ctx, branchID, from, to)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if len(rows) == 0 {
		return &adminapi.RefundCancellationStats{BranchID: branchID}, nil
	}
	return &rows[0], nil
}

func (s *Service) BranchAmendmentLog(ctx context.Context, claims *auth.StaffClaims, branchID string, from, to time.Time, page, limit int) ([]adminapi.AmendmentLogEntry, int, error) {
	if _, err := s.requireBranchAccess(ctx, claims, branchID); err != nil {
		return nil, 0, err
	}
	out, total, err := s.store.AmendmentLog(ctx, branchID, from, to, page, limit)
	if err != nil {
		return nil, 0, apperr.Internal(err)
	}
	return out, total, nil
}

// --- Network dashboard & reports (REP-01 to REP-14) -------------------------
//
// Every method below requires PermReportsNetwork (super_admin, finance) —
// callers below that scope have PermReportsOwnBranch instead.

func (s *Service) requireNetworkReports(claims *auth.StaffClaims) error {
	if !tenant.HasPermission(claims, tenant.PermReportsNetwork) {
		return apperr.PermissionDenied("Network-wide reporting requires the finance or super admin role.")
	}
	return nil
}

// resolveNetworkScope decides which branch a network report is narrowed to.
//
// Brand-wide callers (super admin, finance) may ask for any branch, or none
// for the whole network. Anyone below that — a branch manager or other
// branch-scoped role — is still allowed onto these screens, but only ever
// sees their own scope: an explicit branchId is checked against what they
// hold, and with none given they are pinned to their own branch. Leaving the
// filter off can therefore never widen what a caller sees.
func (s *Service) resolveNetworkScope(ctx context.Context, claims *auth.StaffClaims, branchID string) (string, error) {
	if tenant.HasPermission(claims, tenant.PermReportsNetwork) {
		if branchID != "" {
			if _, err := s.requireBranchAccess(ctx, claims, branchID); err != nil {
				return "", err
			}
		}
		return branchID, nil
	}

	if !tenant.HasPermission(claims, tenant.PermReportsOwnBranch) {
		return "", s.requireNetworkReports(claims)
	}
	if branchID != "" {
		b, err := s.requireBranchAccess(ctx, claims, branchID)
		if err != nil {
			return "", err
		}
		return b.ID, nil
	}
	_, ownBranch := tenant.OwnScope(claims)
	if ownBranch == "" {
		return "", apperr.PermissionDenied("You do not have a branch to report on.")
	}
	return ownBranch, nil
}

// NetworkDashboard backs the dashboard cards and the live pipeline (in
// production / ready / out for delivery). branchID narrows every figure;
// see resolveNetworkScope for who may ask for what.
func (s *Service) NetworkDashboard(ctx context.Context, claims *auth.StaffClaims, branchID string) (*adminapi.NetworkDashboard, error) {
	branchID, err := s.resolveNetworkScope(ctx, claims, branchID)
	if err != nil {
		return nil, err
	}
	d, err := s.store.NetworkDashboard(ctx, today(), branchID)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return d, nil
}

var validTrendUnits = map[string]bool{"day": true, "week": true, "month": true}

// RevenueTrend is sales over time for the whole network ("all") or one
// branch, with the overall totals for the period and a per-branch breakdown.
//
// branchID "" or "all" means every branch. Scope follows the same rules as
// the network dashboard (resolveNetworkScope): brand-wide roles may pick any
// branch or all of them; branch-scoped roles only ever see their own.
func (s *Service) RevenueTrend(ctx context.Context, claims *auth.StaffClaims, branchID, unit string, from, to time.Time) (*adminapi.RevenueTrend, error) {
	if strings.EqualFold(strings.TrimSpace(branchID), "all") {
		branchID = ""
	}
	branchID, err := s.resolveNetworkScope(ctx, claims, strings.TrimSpace(branchID))
	if err != nil {
		return nil, err
	}
	if unit == "" {
		unit = "day"
	}
	if !validTrendUnits[unit] {
		return nil, apperr.Validation("unit must be one of day, week, month.")
	}

	points, err := s.store.RevenueTrend(ctx, branchID, unit, from, to)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	branches, err := s.store.RevenueByBranch(ctx, branchID, from, to)
	if err != nil {
		return nil, apperr.Internal(err)
	}

	out := &adminapi.RevenueTrend{
		Scope:      "all",
		BranchName: "All branches",
		Unit:       unit,
		From:       from.Format("2006-01-02"),
		To:         to.AddDate(0, 0, -1).Format("2006-01-02"), // inclusive, as asked for
		Points:     points,
		Branches:   branches,
	}
	if out.Points == nil {
		out.Points = []adminapi.RevenueTrendPoint{}
	}
	if branchID != "" {
		out.Scope = "branch"
		out.BranchID = branchID
		if len(branches) > 0 {
			out.BranchName = branches[0].BranchName
		}
	}

	// Totals come from the branch breakdown, not the points, so the headline
	// and the per-branch rows can never disagree.
	for _, b := range branches {
		out.Totals.OrderCount += b.OrderCount
		out.Totals.RevenueCents += b.RevenueCents
	}
	if out.Totals.OrderCount > 0 {
		out.Totals.AverageOrderCents = out.Totals.RevenueCents / int64(out.Totals.OrderCount)
	}
	for i := range out.Branches {
		if out.Totals.RevenueCents > 0 {
			pct := float64(out.Branches[i].RevenueCents) * 100 / float64(out.Totals.RevenueCents)
			out.Branches[i].SharePct = math.Round(pct*10) / 10
		}
	}
	return out, nil
}

func (s *Service) NetworkProductPerformance(ctx context.Context, claims *auth.StaffClaims, from, to time.Time) ([]adminapi.ProductPerformance, error) {
	if err := s.requireNetworkReports(claims); err != nil {
		return nil, err
	}
	out, err := s.store.ProductPerformance(ctx, "", from, to)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return out, nil
}

func (s *Service) NetworkBranchPerformance(ctx context.Context, claims *auth.StaffClaims, from, to time.Time) ([]adminapi.BranchPerformance, error) {
	if err := s.requireNetworkReports(claims); err != nil {
		return nil, err
	}
	out, err := s.store.BranchPerformance(ctx, from, to)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return out, nil
}

func (s *Service) NetworkFulfilmentBreakdown(ctx context.Context, claims *auth.StaffClaims, branchID string, from, to time.Time) ([]adminapi.FulfilmentBreakdown, error) {
	branchID, err := s.resolveNetworkScope(ctx, claims, branchID)
	if err != nil {
		return nil, err
	}
	out, err := s.store.FulfilmentBreakdown(ctx, branchID, from, to)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return out, nil
}

func (s *Service) NetworkChannelBreakdown(ctx context.Context, claims *auth.StaffClaims, branchID string, from, to time.Time) ([]adminapi.ChannelBreakdown, error) {
	branchID, err := s.resolveNetworkScope(ctx, claims, branchID)
	if err != nil {
		return nil, err
	}
	out, err := s.store.ChannelBreakdown(ctx, branchID, from, to)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return out, nil
}

func (s *Service) NetworkPeakPeriods(ctx context.Context, claims *auth.StaffClaims, from, to time.Time) ([]adminapi.PeakPeriodCell, error) {
	if err := s.requireNetworkReports(claims); err != nil {
		return nil, err
	}
	out, err := s.store.PeakPeriods(ctx, "", from, to)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return out, nil
}

func (s *Service) NetworkRefundStats(ctx context.Context, claims *auth.StaffClaims, from, to time.Time) ([]adminapi.RefundCancellationStats, error) {
	if err := s.requireNetworkReports(claims); err != nil {
		return nil, err
	}
	out, err := s.store.RefundCancellationStats(ctx, "", from, to)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return out, nil
}

// --- Comparable ("like-for-like") sales (REP-06 to REP-09, BRD 11.3) -------

const defaultMaturityMonths = 13

// ComparableSales classifies every branch in scope as either comparable
// (traded in both periods, active, and open at least MaturityMonths before
// the comparison period started) or non-comparable, then totals revenue
// growth across the comparable set only — a new branch's ramp-up curve
// must never distort the network's like-for-like figure.
func (s *Service) ComparableSales(ctx context.Context, claims *auth.StaffClaims, f adminapi.ComparableSalesFilter) (*adminapi.ComparableSalesReport, error) {
	if !tenant.HasPermission(claims, tenant.PermReportsComparable) {
		return nil, apperr.PermissionDenied("Comparable sales reporting requires the finance or super admin role.")
	}
	if f.MaturityMonths <= 0 {
		f.MaturityMonths = defaultMaturityMonths
	}

	branches, err := s.store.ListBranchesForComparison(ctx)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	aggByBranch, err := s.store.OrdersAggByBranch(ctx, f.CurrentFrom, f.CurrentTo, f.CompareFrom, f.CompareTo)
	if err != nil {
		return nil, apperr.Internal(err)
	}

	maturityCutoff := f.CompareFrom.AddDate(0, -f.MaturityMonths, 0)

	// Both slices start empty rather than nil so they marshal as [] and not
	// null: a client that trusts the documented array type should not have to
	// guard against a null it was never promised.
	report := &adminapi.ComparableSalesReport{
		MaturityMonths: f.MaturityMonths,
		Comparable:     []adminapi.ComparableBranch{},
		NonComparable:  []adminapi.NonComparableBranch{},
	}
	for _, b := range branches {
		if b.Status == "suspended" {
			report.NonComparable = append(report.NonComparable, adminapi.NonComparableBranch{BranchID: b.ID, BranchName: b.Name, Reason: "suspended"})
			continue
		}
		if b.Status == "closed" {
			report.NonComparable = append(report.NonComparable, adminapi.NonComparableBranch{BranchID: b.ID, BranchName: b.Name, Reason: "closed"})
			continue
		}
		if !b.CreatedAt.Before(maturityCutoff) {
			report.NonComparable = append(report.NonComparable, adminapi.NonComparableBranch{BranchID: b.ID, BranchName: b.Name, Reason: "new"})
			continue
		}
		agg, ok := aggByBranch[b.ID]
		if !ok || agg.CurrentOrders == 0 || agg.CompareOrders == 0 {
			report.NonComparable = append(report.NonComparable, adminapi.NonComparableBranch{BranchID: b.ID, BranchName: b.Name, Reason: "no_orders_in_period"})
			continue
		}

		cb := adminapi.ComparableBranch{
			BranchID: b.ID, BranchName: b.Name,
			CurrentRevenueCents: agg.CurrentRevenueCents, CurrentOrders: agg.CurrentOrders,
			CompareRevenueCents: agg.CompareRevenueCents, CompareOrders: agg.CompareOrders,
		}
		if agg.CompareRevenueCents > 0 {
			cb.RevenueGrowthPct = (float64(agg.CurrentRevenueCents) - float64(agg.CompareRevenueCents)) / float64(agg.CompareRevenueCents) * 100
		}
		if agg.CompareOrders > 0 {
			cb.OrderGrowthPct = (float64(agg.CurrentOrders) - float64(agg.CompareOrders)) / float64(agg.CompareOrders) * 100
		}
		report.Comparable = append(report.Comparable, cb)
		report.TotalCurrentRevenue += agg.CurrentRevenueCents
		report.TotalCompareRevenue += agg.CompareRevenueCents
	}
	if report.TotalCompareRevenue > 0 {
		report.NetworkRevenueGrowthPct = (float64(report.TotalCurrentRevenue) - float64(report.TotalCompareRevenue)) / float64(report.TotalCompareRevenue) * 100
	}
	return report, nil
}

// --- Customer record management (ADM-19) ------------------------------------

func (s *Service) requireCustomersView(claims *auth.StaffClaims) error {
	if !tenant.HasPermission(claims, tenant.PermCustomersView) {
		return apperr.PermissionDenied("You do not have permission to view customer records.")
	}
	return nil
}

// ListCustomers scopes the listing to the caller: brand-scoped roles see
// every customer; a branch-scoped caller is restricted (in SQL,
// via an EXISTS on orders) to customers who have ordered from their scope.
func (s *Service) ListCustomers(ctx context.Context, claims *auth.StaffClaims, f adminapi.CustomerListFilter) ([]adminapi.CustomerRecord, int, error) {
	if err := s.requireCustomersView(claims); err != nil {
		return nil, 0, err
	}
	if !tenant.IsBrandScoped(claims) {
		f.BranchID = callerScope(claims)
		if f.BranchID == "" {
			return nil, 0, apperr.PermissionDenied("Your role has no branch scope to list customers for.")
		}
	}
	out, total, err := s.store.ListCustomers(ctx, f)
	if err != nil {
		return nil, 0, apperr.Internal(err)
	}
	return out, total, nil
}

// callerScope picks one representative branch from the caller's role
// assignments for the customer listing's single-scope filter. A caller
// holding several branch roles sees the union in practice because
// GetCustomerDetail/notes are checked per-customer via
// CustomerHasOrderInScope, not by this coarse listing filter alone.
// Franchise-scope assignments no longer exist (migration 0070) and are
// ignored if an older token still carries one.
func callerScope(claims *auth.StaffClaims) string {
	for _, ra := range claims.RoleAssignments {
		if ra.ScopeType == tenant.ScopeBranch && ra.BranchID != nil && *ra.BranchID != "" {
			return *ra.BranchID
		}
	}
	return ""
}

func (s *Service) requireCustomerAccess(ctx context.Context, claims *auth.StaffClaims, customerID string) error {
	if tenant.IsBrandScoped(claims) {
		return nil
	}
	for _, ra := range claims.RoleAssignments {
		if ra.ScopeType != tenant.ScopeBranch || ra.BranchID == nil || *ra.BranchID == "" {
			continue
		}
		ok, err := s.store.CustomerHasOrderInScope(ctx, customerID, *ra.BranchID)
		if err != nil {
			return apperr.Internal(err)
		}
		if ok {
			return nil
		}
	}
	return apperr.PermissionDenied("This customer has no order history in your scope.")
}

func (s *Service) GetCustomer(ctx context.Context, claims *auth.StaffClaims, customerID string) (*adminapi.CustomerRecord, error) {
	if err := s.requireCustomersView(claims); err != nil {
		return nil, err
	}
	if err := s.requireCustomerAccess(ctx, claims, customerID); err != nil {
		return nil, err
	}
	var branchID string
	if !tenant.IsBrandScoped(claims) {
		branchID = callerScope(claims)
	}
	c, err := s.store.GetCustomerDetail(ctx, customerID, branchID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, apperr.NotFound("Customer not found.")
		}
		return nil, apperr.Internal(err)
	}
	return c, nil
}

func (s *Service) GetCustomerOrders(ctx context.Context, claims *auth.StaffClaims, customerID string) ([]adminapi.CustomerOrderSummary, error) {
	if err := s.requireCustomersView(claims); err != nil {
		return nil, err
	}
	if err := s.requireCustomerAccess(ctx, claims, customerID); err != nil {
		return nil, err
	}
	var branchID string
	if !tenant.IsBrandScoped(claims) {
		branchID = callerScope(claims)
	}
	out, err := s.store.ListCustomerOrders(ctx, customerID, branchID)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return out, nil
}

func (s *Service) GetCustomerNotes(ctx context.Context, claims *auth.StaffClaims, customerID string) ([]adminapi.CustomerNote, error) {
	if err := s.requireCustomersView(claims); err != nil {
		return nil, err
	}
	if err := s.requireCustomerAccess(ctx, claims, customerID); err != nil {
		return nil, err
	}
	out, err := s.store.ListCustomerNotes(ctx, customerID)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return out, nil
}

func (s *Service) AddCustomerNote(ctx context.Context, claims *auth.StaffClaims, customerID, note string) (*adminapi.CustomerNote, error) {
	if err := s.requireCustomersView(claims); err != nil {
		return nil, err
	}
	if err := s.requireCustomerAccess(ctx, claims, customerID); err != nil {
		return nil, err
	}
	if note == "" {
		return nil, apperr.Validation("note must not be empty.")
	}
	n, err := s.store.CreateCustomerNote(ctx, customerID, claims.StaffID, note)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return n, nil
}

func (s *Service) AddCustomerTag(ctx context.Context, claims *auth.StaffClaims, customerID, tag string) error {
	if err := s.requireCustomersView(claims); err != nil {
		return err
	}
	if err := s.requireCustomerAccess(ctx, claims, customerID); err != nil {
		return err
	}
	if tag == "" {
		return apperr.Validation("tag must not be empty.")
	}
	if err := s.store.AddCustomerTag(ctx, customerID, tag, claims.StaffID); err != nil {
		return apperr.Internal(err)
	}
	return nil
}

func (s *Service) RemoveCustomerTag(ctx context.Context, claims *auth.StaffClaims, customerID, tag string) error {
	if err := s.requireCustomersView(claims); err != nil {
		return err
	}
	if err := s.requireCustomerAccess(ctx, claims, customerID); err != nil {
		return err
	}
	if err := s.store.RemoveCustomerTag(ctx, customerID, tag); err != nil {
		return apperr.Internal(err)
	}
	return nil
}

// --- Network-wide audit search (SA-15) --------------------------------------

func (s *Service) SearchAuditLog(ctx context.Context, claims *auth.StaffClaims, f adminapi.AuditSearchFilter) ([]adminapi.AuditEntry, int, error) {
	if !tenant.IsBrandScoped(claims) {
		return nil, 0, apperr.PermissionDenied("Network-wide audit search requires a brand-scoped role.")
	}
	out, total, err := s.store.SearchAuditLog(ctx, f)
	if err != nil {
		return nil, 0, apperr.Internal(err)
	}
	return out, total, nil
}
