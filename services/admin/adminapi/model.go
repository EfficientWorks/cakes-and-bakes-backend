// Package admin implements cross-cutting, read-heavy reporting and
// dashboards (BRD Section 11): branch dashboards, the network
// dashboard, comparable ("like-for-like") sales, product/channel/fulfilment
// breakdowns, peak-period analysis, network-wide audit search, and
// franchise-scoped customer record management. Because these queries
// aggregate across orders/refunds/customers/audit_log regardless of which
// module owns the write path, this module reads those tables directly via
// SQL rather than composing many small cross-module Go calls.
package adminapi

import (
	"encoding/json"
	"time"
)

// --- Branch dashboard & reports (ADM-20, ADM-21) ----------------------------

type BranchDashboard struct {
	BranchID          string         `json:"branchId"`
	TodayRevenueCents int64          `json:"todayRevenueCents"`
	TodayOrders       int            `json:"todayOrders"`
	TodayAOVCents     int64          `json:"todayAovCents"`
	StatusCounts      map[string]int `json:"statusCounts"`
	UpcomingSlotLoad  []SlotLoad     `json:"upcomingSlotLoad"`
}

type SlotLoad struct {
	Date           string `json:"date"`
	SlotStart      string `json:"slotStart"`
	FulfilmentType string `json:"fulfilmentType"`
	MaxCapacity    int    `json:"maxCapacity"`
	ReservedCount  int    `json:"reservedCount"`
}

type SalesPoint struct {
	Date         string `json:"date"`
	OrderCount   int    `json:"orderCount"`
	RevenueCents int64  `json:"revenueCents"`
}

type ProductPerformance struct {
	ProductID    string `json:"productId"`
	ProductName  string `json:"productName"`
	QuantitySold int    `json:"quantitySold"`
	RevenueCents int64  `json:"revenueCents"`
}

type FulfilmentPerformance struct {
	BranchID        string  `json:"branchId"`
	CompletedOrders int     `json:"completedOrders"`
	AvgPrepMinutes  float64 `json:"avgPrepMinutes"`
	OnTimeCount     int     `json:"onTimeCount"`
	OnTimeRatePct   float64 `json:"onTimeRatePct"`
}

type RefundCancellationStats struct {
	BranchID        string  `json:"branchId"`
	BranchName      string  `json:"branchName,omitempty"`
	TotalOrders     int     `json:"totalOrders"`
	CancelledOrders int     `json:"cancelledOrders"`
	CancellationPct float64 `json:"cancellationRatePct"`
	RefundedOrders  int     `json:"refundedOrders"`
	RefundedCents   int64   `json:"refundedCents"`
	RefundRatePct   float64 `json:"refundRatePct"`
}

type AmendmentLogEntry struct {
	ID              string          `json:"id"`
	OrderID         string          `json:"orderId"`
	OrderReference  string          `json:"orderReference"`
	ActorID         *string         `json:"actorId"`
	ActorRole       string          `json:"actorRole"`
	Reason          string          `json:"reason"`
	BeforeValue     json.RawMessage `json:"beforeValue"`
	AfterValue      json.RawMessage `json:"afterValue"`
	PriceDeltaCents int64           `json:"priceDeltaCents"`
	CreatedAt       time.Time       `json:"createdAt"`
}

// --- Network dashboard (REP-01, REP-02) -------------------------------------

type NetworkDashboard struct {
	TodayRevenueCents int64             `json:"todayRevenueCents"`
	TodayOrders       int               `json:"todayOrders"`
	TodayAOVCents     int64             `json:"todayAovCents"`
	Pipeline          map[string]int    `json:"pipeline"` // status -> count, "in-flight" orders only
	BranchLeague      []BranchLeagueRow `json:"branchLeague"`
}

type BranchLeagueRow struct {
	BranchID     string `json:"branchId"`
	BranchName   string `json:"branchName"`
	OrderCount   int    `json:"orderCount"`
	RevenueCents int64  `json:"revenueCents"`
}

// --- Revenue trend (REP-03/04/05) -------------------------------------------

type RevenueTrendPoint struct {
	Period       string `json:"period"`
	OrderCount   int    `json:"orderCount"`
	RevenueCents int64  `json:"revenueCents"`
}

// RevenueTrendTotals is the overall sale for the whole period in scope.
type RevenueTrendTotals struct {
	OrderCount        int   `json:"orderCount"`
	RevenueCents      int64 `json:"revenueCents"`
	AverageOrderCents int64 `json:"averageOrderCents"`
}

// RevenueTrendBranch is one branch's share of the period's sales.
type RevenueTrendBranch struct {
	BranchID     string  `json:"branchId"`
	BranchName   string  `json:"branchName"`
	OrderCount   int     `json:"orderCount"`
	RevenueCents int64   `json:"revenueCents"`
	SharePct     float64 `json:"sharePct"`
}

// RevenueTrend is the revenue-trend report: all branches or one.
//
// Scope is "all" (the whole network) or "branch". Points are zero-filled so
// every period in the range appears, which is what a chart needs. Branches is
// the per-branch breakdown of the same period — every branch when the scope
// is "all", just the one otherwise.
type RevenueTrend struct {
	Scope      string               `json:"scope"`
	BranchID   string               `json:"branchId,omitempty"`
	BranchName string               `json:"branchName"`
	Unit       string               `json:"unit"`
	From       string               `json:"from"`
	To         string               `json:"to"`
	Totals     RevenueTrendTotals   `json:"totals"`
	Points     []RevenueTrendPoint  `json:"points"`
	Branches   []RevenueTrendBranch `json:"branches"`
}

// --- Comparable ("like-for-like") sales (REP-06 to REP-09, BRD 11.3) -------

// ComparableSalesFilter scopes a like-for-like sales comparison between two
// periods. MaturityMonths defaults to 13 (BRD 11.3): a branch must have
// been trading at least this long before the comparison period begins to
// be eligible, so a newly opened branch's ramp-up curve never distorts the
// network comparison.
type ComparableSalesFilter struct {
	CurrentFrom, CurrentTo time.Time
	CompareFrom, CompareTo time.Time
	MaturityMonths         int
}

type ComparableBranch struct {
	BranchID            string  `json:"branchId"`
	BranchName          string  `json:"branchName"`
	CurrentRevenueCents int64   `json:"currentRevenueCents"`
	CurrentOrders       int     `json:"currentOrders"`
	CompareRevenueCents int64   `json:"compareRevenueCents"`
	CompareOrders       int     `json:"compareOrders"`
	RevenueGrowthPct    float64 `json:"revenueGrowthPct"`
	OrderGrowthPct      float64 `json:"orderGrowthPct"`
}

type NonComparableBranch struct {
	BranchID   string `json:"branchId"`
	BranchName string `json:"branchName"`
	Reason     string `json:"reason"` // new | suspended | closed | no_orders_in_period
}

type ComparableSalesReport struct {
	MaturityMonths          int                   `json:"maturityMonths"`
	Comparable              []ComparableBranch    `json:"comparable"`
	NonComparable           []NonComparableBranch `json:"nonComparable"`
	TotalCurrentRevenue     int64                 `json:"totalCurrentRevenueCents"`
	TotalCompareRevenue     int64                 `json:"totalCompareRevenueCents"`
	NetworkRevenueGrowthPct float64               `json:"networkRevenueGrowthPct"`
}

// --- Channel / peak-period (REP-11, REP-13) ---------------------------------

type ChannelBreakdown struct {
	Channel      string `json:"channel"`
	OrderCount   int    `json:"orderCount"`
	RevenueCents int64  `json:"revenueCents"`
}

// FulfilmentBreakdown is how orders were handed over: collected later,
// delivered, or taken away at the counter.
type FulfilmentBreakdown struct {
	FulfilmentType string `json:"fulfilmentType"`
	OrderCount     int    `json:"orderCount"`
	RevenueCents   int64  `json:"revenueCents"`
}

// BranchPerformance is one outlet's trading over a chosen window.
//
// Distinct from the dashboard's league table, which is fixed to today, and
// from comparable sales, which deliberately withholds figures for outlets too
// young to compare. This answers the plainer question the reporting screen
// asks: how did these outlets do over the period on screen.
type BranchPerformance struct {
	BranchID   string `json:"branchId"`
	BranchName string `json:"branchName"`
	// "corporate_store" or "franchise_store" — replaces the old franchise
	// name column now that branches no longer sit under a franchise row.
	OwnershipType string `json:"ownershipType"`
	OrderCount    int    `json:"orderCount"`
	RevenueCents  int64  `json:"revenueCents"`
	// Average order value, computed here so every reader agrees on the
	// rounding rather than each dividing for itself.
	AverageOrderValueCents int64 `json:"averageOrderValueCents"`
}

type PeakPeriodCell struct {
	DayOfWeek    int   `json:"dayOfWeek"` // 0=Sunday..6=Saturday
	Hour         int   `json:"hour"`
	OrderCount   int   `json:"orderCount"`
	RevenueCents int64 `json:"revenueCents"`
}

// --- Customer record management (ADM-19) ------------------------------------

type CustomerRecord struct {
	ID              string     `json:"id"`
	Email           string     `json:"email"`
	FirstName       string     `json:"firstName"`
	LastName        string     `json:"lastName"`
	MobileNumber    *string    `json:"mobileNumber"`
	IsVerified      bool       `json:"isVerified"`
	CreatedAt       time.Time  `json:"createdAt"`
	TotalOrders     int        `json:"totalOrders"`
	TotalSpendCents int64      `json:"totalSpendCents"`
	LastOrderAt     *time.Time `json:"lastOrderAt"`
	Tags            []string   `json:"tags"`
}

type CustomerOrderSummary struct {
	ID        string `json:"id"`
	Reference string `json:"reference"`
	// The store's bill number, the number staff and customers know the
	// order by; orderNo is the same number (as the POS and KDS call it).
	// Null on an order that never got one.
	BillNo         *int      `json:"billNo"`
	OrderNo        *int      `json:"orderNo"`
	BranchID       string    `json:"branchId"`
	BranchName     string    `json:"branchName"`
	Status         string    `json:"status"`
	FulfilmentType string    `json:"fulfilmentType"`
	RecipientName  string    `json:"recipientName"`
	ScheduledDate  string    `json:"scheduledDate"`
	SlotStart      string    `json:"slotStart"`
	ItemCount      int       `json:"itemCount"`
	TotalCents     int64     `json:"totalCents"`
	CreatedAt      time.Time `json:"createdAt"`
}

type CustomerNote struct {
	ID            string    `json:"id"`
	CustomerID    string    `json:"customerId"`
	AuthorStaffID string    `json:"authorStaffId"`
	Note          string    `json:"note"`
	CreatedAt     time.Time `json:"createdAt"`
}

type CustomerListFilter struct {
	Search   string
	BranchID string
	// "web" or "pos" — narrows to customers who have ordered that way.
	// Empty means both, which is most of them.
	Source string
	Page   int
	Limit  int
}

// --- Network-wide audit search (SA-15) --------------------------------------

type AuditSearchFilter struct {
	ActorID      string
	ActorType    string
	Action       string
	ResourceType string
	BranchID     string
	DateFrom     *time.Time
	DateTo       *time.Time
	Page         int
	Limit        int
}

type AuditEntry struct {
	ID           string          `json:"id"`
	ActorID      *string         `json:"actorId"`
	ActorType    string          `json:"actorType"`
	ActorRole    string          `json:"actorRole"`
	Action       string          `json:"action"`
	ResourceType string          `json:"resourceType"`
	ResourceID   *string         `json:"resourceId"`
	BranchID     *string         `json:"branchId"`
	BeforeValue  json.RawMessage `json:"beforeValue,omitempty"`
	AfterValue   json.RawMessage `json:"afterValue,omitempty"`
	Reason       string          `json:"reason,omitempty"`
	CreatedAt    time.Time       `json:"createdAt"`
}

// --- Store-layer helper shapes -----------------------------------------------

// BranchInfo is the subset of franchise.Branch the comparable-sales
// calculation needs; admin reads it directly rather than depending on the
// tenancy package, matching this module's direct-SQL architecture.
type BranchInfo struct {
	ID        string
	Name      string
	Status    string
	CreatedAt time.Time
}

// BranchOrderAgg holds the current- and comparison-period order totals for
// one branch, keyed by branch ID in the map ComparableSales works from.
type BranchOrderAgg struct {
	CurrentRevenueCents int64
	CurrentOrders       int
	CompareRevenueCents int64
	CompareOrders       int
}
