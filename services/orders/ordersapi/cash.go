package ordersapi

import "time"

// Cash movement kinds: money into and out of the drawer that is not a sale.
const (
	CashPayIn  = "payin"
	CashPayOut = "payout"
)

/*
 * CashDay is one outlet's drawer on one trading day.
 *
 * StartingCash is the float the drawer opens with. CarryForward is the "apply
 * the same amount daily" tick: with it set, later days that nobody has opened
 * explicitly inherit this amount rather than starting at zero, which is what a
 * shop floating the same amount every morning actually wants.
 */
type CashDay struct {
	BranchID     string `json:"branchId"`
	BusinessDate string `json:"businessDate"`

	StartingCashCents int64 `json:"startingCashCents"`
	CarryForward      bool  `json:"carryForward"`
	// True when this day has no record of its own and the amount came from an
	// earlier day's standing float. The screen should say so rather than
	// implying somebody counted it this morning.
	StartingCashInherited bool `json:"startingCashInherited"`

	ClosedAt          *time.Time `json:"closedAt,omitempty"`
	CountedCashCents  *int64     `json:"countedCashCents,omitempty"`
	ExpectedCashCents *int64     `json:"expectedCashCents,omitempty"`
	ShortageCents     *int64     `json:"shortageCents,omitempty"`
	Note              string     `json:"note,omitempty"`
}

// CashMovement is one pay-in or pay-out.
type CashMovement struct {
	ID           string    `json:"id"`
	BranchID     string    `json:"branchId"`
	BusinessDate string    `json:"businessDate"`
	Kind         string    `json:"kind"`
	AmountCents  int64     `json:"amountCents"`
	Reason       string    `json:"reason,omitempty"`
	CreatedBy    string    `json:"createdBy,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

// TenderTotal is what one payment method took in over a period.
type TenderTotal struct {
	Method      string `json:"method"`
	AmountCents int64  `json:"amountCents"`
	Count       int    `json:"count"`
}

/*
 * DayCloseSummary is the drawer reconciliation for one day.
 *
 * The arithmetic the screen shows:
 *
 *	expected = startingCash + cashSales + payIns - payOuts
 *	shortage = counted - expected        (negative means money is missing)
 *
 * Only cash takes part. Card, UPI and anything else are reported alongside
 * because the day's takings are worth seeing whole, but they never reach the
 * drawer and so cannot explain what is or is not in it.
 */
type DayCloseSummary struct {
	BranchID     string `json:"branchId"`
	BusinessDate string `json:"businessDate"`

	StartingCashCents     int64 `json:"startingCashCents"`
	StartingCashInherited bool  `json:"startingCashInherited"`
	CashSalesCents        int64 `json:"cashSalesCents"`
	PayInsCents           int64 `json:"payInsCents"`
	PayOutsCents          int64 `json:"payOutsCents"`
	ExpectedCashCents     int64 `json:"expectedCashCents"`

	// Null until somebody counts the drawer.
	CountedCashCents *int64 `json:"countedCashCents,omitempty"`
	ShortageCents    *int64 `json:"shortageCents,omitempty"`

	// Context, not drawer money.
	NonCash           []TenderTotal `json:"nonCash"`
	NonCashCents      int64         `json:"nonCashCents"`
	TotalTakingsCents int64         `json:"totalTakingsCents"`
	OrderCount        int           `json:"orderCount"`

	ClosedAt *time.Time `json:"closedAt,omitempty"`
	Note     string     `json:"note,omitempty"`
	IsClosed bool       `json:"isClosed"`
}

// DailyReportRow is one day in the history report.
type DailyReportRow struct {
	BusinessDate       string `json:"businessDate"`
	OrderCount         int    `json:"orderCount"`
	GrossSalesCents    int64  `json:"grossSalesCents"`
	DiscountCents      int64  `json:"discountCents"`
	TaxCents           int64  `json:"taxCents"`
	ServiceChargeCents int64  `json:"serviceChargeCents"`
	NetSalesCents      int64  `json:"netSalesCents"`
	RefundCents        int64  `json:"refundCents"`

	CancelledOrderCount int   `json:"cancelledOrderCount"`
	CancelledOrderCents int64 `json:"cancelledOrderCents"`
	// Orders handed back in full. The amount is what the order came to —
	// goods, tax and delivery net of discount — without the tip, which is
	// the staff's whatever became of the order.
	ReturnedOrderCount  int   `json:"returnedOrderCount"`
	ReturnedOrderCents  int64 `json:"returnedOrderCents"`
	DeliveryChargeCents int64 `json:"deliveryChargeCents"`

	CashCents   int64 `json:"cashCents"`
	CardCents   int64 `json:"cardCents"`
	OnlineCents int64 `json:"onlineCents"`
	OnlineCount int   `json:"onlineCount"`

	ShortageCents *int64 `json:"shortageCents,omitempty"`
	IsClosed      bool   `json:"isClosed"`
}

// SummaryLine is one row with both a count and an amount.
type SummaryLine struct {
	Label       string `json:"label"`
	Count       int    `json:"count"`
	AmountCents int64  `json:"amountCents"`
}

// CategorySales is one product category's totals.
type CategorySales struct {
	CategoryID   string `json:"categoryId"`
	CategoryName string `json:"categoryName"`
	Quantity     int    `json:"quantity"`
	AmountCents  int64  `json:"amountCents"`
}

// ReturnItemLine is one returned product line.
type ReturnItemLine struct {
	OrderID          string `json:"orderId"`
	OrderReference   string `json:"orderReference"`
	ProductID        string `json:"productId"`
	ProductName      string `json:"productName"`
	ReturnedQuantity int    `json:"returnedQuantity"`
	AmountCents      int64  `json:"amountCents"`
	OrderStatus      string `json:"orderStatus"`
}

// OpenOrdersSummary is what's running right now at a branch.
type OpenOrdersSummary struct {
	Count       int   `json:"count"`
	AmountCents int64 `json:"amountCents"`
}

// DailyReport is the history summary over a range of days.
type DailyReport struct {
	BranchID string           `json:"branchId"`
	From     string           `json:"from"`
	To       string           `json:"to"`
	Days     []DailyReportRow `json:"days"`
	Totals   DailyReportRow   `json:"totals"`

	OpenOrders OpenOrdersSummary `json:"openOrders"`
	Hours      []HourlyReportRow `json:"hours,omitempty"`

	SalesSummary       []SummaryLine    `json:"salesSummary"`
	TransactionSummary []SummaryLine    `json:"transactionSummary"`
	TopItems           []ProductSales   `json:"topItems"`
	TopCategories      []CategorySales  `json:"topCategories"`
	ReturnItems        []ReturnItemLine `json:"returnItems"`
}

// HourlyReportRow is one hour-of-day bucket, used only when a report is
// requested for a single day (from == to) — the same day the person is
// currently looking at gets an intraday breakdown instead of one flat row.
type HourlyReportRow struct {
	HourLabel     string `json:"hourLabel"` // "1AM", "3AM" ... "11PM"
	Hour          int    `json:"hour"`      // 0-23, 24-hour, for sorting
	OrderCount    int    `json:"orderCount"`
	NetSalesCents int64  `json:"netSalesCents"`
}
type ProductVariantSales struct {
	VariantID   string `json:"variantId"`
	VariantName string `json:"variantName"`
	Quantity    int    `json:"quantity"`
	AmountCents int64  `json:"amountCents"`
}

// ProductSales is one product's contribution over a period.
// ProductModifierSales is one chosen option (a variation such as a flavour,
// or an add-on) totalled across the units it was sold on. AmountCents is the
// option's own price delta times those units — what it added to the bill,
// not the price of the products it was chosen on.
type ProductModifierSales struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Quantity    int    `json:"quantity"`
	AmountCents int64  `json:"amountCents"`
}

type ProductSales struct {
	ProductID   string                `json:"productId"`
	ProductName string                `json:"productName"`
	Quantity    int                   `json:"quantity"`
	AmountCents int64                 `json:"amountCents"`
	Variants    []ProductVariantSales `json:"variants,omitempty"`
	// Variations are the chosen "variation"-kind options (flavours),
	// counted per unit sold. Add-ons are not part of the best-seller list.
	Variations []ProductModifierSales `json:"variations,omitempty"`
}

// CashDayFilter picks one outlet's day.
type CashDayFilter struct {
	BranchID     string
	BusinessDate string
}

// StartingCashInput sets the opening float.
type StartingCashInput struct {
	BranchID     string
	BusinessDate string
	AmountCents  int64
	// The "apply the same amount daily" tick.
	CarryForward bool
}

// CashMovementInput records a pay-in or a pay-out.
type CashMovementInput struct {
	BranchID     string
	BusinessDate string
	Kind         string
	AmountCents  int64
	Reason       string
}

// DayCloseInput is the counted drawer.
type DayCloseInput struct {
	BranchID         string
	BusinessDate     string
	CountedCashCents int64
	Note             string
}
