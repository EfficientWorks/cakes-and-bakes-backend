package report

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/orders/ordersapi"
)

/*
 * DailyReportRows is the history summary, one row per day the outlet traded.
 *
 * Built from orders for what was sold and from bill_payments for how it was
 * paid, joined by date rather than by order: a bill split three ways across
 * two tenders is three payment rows against one order, and counting the order
 * once while summing the payments separately is the only way both halves come
 * out right.
 */
func (s *Repo) DailyReportRows(ctx context.Context, branchID, from, to string) ([]ordersapi.DailyReportRow, error) {
	q := `
		WITH days AS (
		    SELECT generate_series($2::date, $3::date, '1 day')::date AS d
		),
		sales AS (
		    -- A sale is a completed order. Open orders (still being worked
		    -- on), cancellations and returns are reported on their own lines
		    -- and must not inflate the sales count or amounts — they used to,
		    -- because "not cancelled" counted every order still on the floor.
		    SELECT (` + orderTimeSQL + ` AT TIME ZONE b.timezone)::date AS d,
		           COUNT(*) FILTER (WHERE ` + settledOrderSQL + `)  AS order_count,
		           COALESCE(SUM(o.subtotal_cents) FILTER (WHERE ` + settledOrderSQL + `), 0) AS gross,
		           COALESCE(SUM(o.discount_cents) FILTER (WHERE ` + settledOrderSQL + `), 0) AS discount,
		           COALESCE(SUM(o.tax_cents) FILTER (WHERE ` + settledOrderSQL + `), 0)      AS tax,
		           COALESCE(SUM(o.service_charge_cents) FILTER (WHERE ` + settledOrderSQL + `), 0) AS service,
		           COALESCE(SUM(o.delivery_fee_cents) FILTER (WHERE ` + settledOrderSQL + `), 0)   AS delivery,
		           -- Net is what the settled orders came to, tax included.
		           COALESCE(SUM(o.total_cents) FILTER (WHERE ` + settledOrderSQL + `), 0)    AS net,
		           COALESCE(SUM(o.total_cents) FILTER (WHERE o.payment_status IN ('refunded','partial_refund')), 0) AS refunded,
		           COUNT(*) FILTER (WHERE o.status = 'cancelled')  AS cancelled_count,
		           COALESCE(SUM(o.total_cents) FILTER (WHERE o.status = 'cancelled'), 0) AS cancelled_amount,
		           COUNT(*) FILTER (WHERE o.status = 'returned')  AS returned_count,
		           COALESCE(SUM(` + returnedOrderAmountSQL + `) FILTER (WHERE o.status = 'returned'), 0) AS returned_amount,
		           COALESCE(SUM(o.total_cents) FILTER (
		               WHERE o.channel <> 'pos' AND ` + settledOrderSQL + `), 0) AS online,
		           COUNT(*) FILTER (WHERE o.channel <> 'pos' AND ` + settledOrderSQL + `) AS online_count
		      FROM orders o
		      JOIN branches b ON b.id = o.branch_id
		     WHERE o.branch_id = $1
		       AND o.deleted_at IS NULL
		       AND (` + orderTimeSQL + ` AT TIME ZONE b.timezone)::date BETWEEN $2::date AND $3::date
		     GROUP BY 1
		),
		tenders AS (
		    SELECT (p.created_at AT TIME ZONE b.timezone)::date AS d,
		           COALESCE(SUM(p.amount_cents) FILTER (WHERE p.method = 'cash'), 0) AS cash,
		           COALESCE(SUM(p.amount_cents) FILTER (WHERE p.method = 'card'), 0) AS card
		      FROM bill_payments p
		      JOIN orders o   ON o.id = p.order_id
		      JOIN branches b ON b.id = o.branch_id
		     WHERE o.branch_id = $1
		       AND o.deleted_at IS NULL
		       -- Money taken on an order later cancelled or returned went
		       -- back out again; only settled orders count as takings.
		       AND ` + settledOrderSQL + `
		       AND (p.created_at AT TIME ZONE b.timezone)::date BETWEEN $2::date AND $3::date
		     GROUP BY 1
		)
		SELECT to_char(days.d, 'YYYY-MM-DD'),
		       COALESCE(sales.order_count, 0),
		       COALESCE(sales.gross, 0), COALESCE(sales.discount, 0),
		       COALESCE(sales.tax, 0), COALESCE(sales.service, 0),
		       COALESCE(sales.net, 0), COALESCE(sales.refunded, 0),
		       COALESCE(sales.cancelled_count, 0), COALESCE(sales.cancelled_amount, 0),
		       COALESCE(sales.returned_count, 0), COALESCE(sales.returned_amount, 0),
		       COALESCE(sales.delivery, 0),
		       COALESCE(tenders.cash, 0), COALESCE(tenders.card, 0),
		       COALESCE(sales.online, 0), COALESCE(sales.online_count, 0),
		       cd.shortage_cents,
		       (cd.closed_at IS NOT NULL)
		  FROM days
		  LEFT JOIN sales   ON sales.d   = days.d
		  LEFT JOIN tenders ON tenders.d = days.d
		  LEFT JOIN pos_cash_days cd ON cd.business_date = days.d AND cd.branch_id = $1
		 ORDER BY days.d`

	rows, err := s.pool.Query(ctx, q, branchID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ordersapi.DailyReportRow{}
	for rows.Next() {
		var r ordersapi.DailyReportRow
		var closed *bool
		if err := rows.Scan(&r.BusinessDate, &r.OrderCount,
			&r.GrossSalesCents, &r.DiscountCents, &r.TaxCents, &r.ServiceChargeCents,
			&r.NetSalesCents, &r.RefundCents,
			&r.CancelledOrderCount, &r.CancelledOrderCents,
			&r.ReturnedOrderCount, &r.ReturnedOrderCents, &r.DeliveryChargeCents,
			&r.CashCents, &r.CardCents, &r.OnlineCents, &r.OnlineCount,
			&r.ShortageCents, &closed); err != nil {
			return nil, err
		}
		r.IsClosed = closed != nil && *closed
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Repo) SalesSummaryForRange(ctx context.Context, branchID, from, to string) ([]ordersapi.SummaryLine, error) {
	q := `
		SELECT
		    -- Sales are settled orders only; open, cancelled and returned
		    -- orders each have their own line.
		    COUNT(*) FILTER (WHERE ` + settledOrderSQL + `),
		    COALESCE(SUM(o.subtotal_cents) FILTER (WHERE ` + settledOrderSQL + `), 0),
		    COUNT(*) FILTER (WHERE o.status = 'cancelled'),
		    COALESCE(SUM(o.total_cents) FILTER (WHERE o.status = 'cancelled'), 0),
		    COUNT(*) FILTER (WHERE o.status = 'returned'),
		    COALESCE(SUM(` + returnedOrderAmountSQL + `) FILTER (WHERE o.status = 'returned'), 0),
		    COUNT(*) FILTER (WHERE ` + settledOrderSQL + ` AND o.tax_cents > 0),
		    COALESCE(SUM(o.tax_cents) FILTER (WHERE ` + settledOrderSQL + `), 0),
		    COUNT(*) FILTER (WHERE ` + settledOrderSQL + ` AND o.delivery_fee_cents > 0),
		    COALESCE(SUM(o.delivery_fee_cents) FILTER (WHERE ` + settledOrderSQL + `), 0),
		    -- Coupons are the coded discounts; Discount is everything taken
		    -- off, coded or by hand at the till.
		    COUNT(*) FILTER (WHERE ` + settledOrderSQL + ` AND o.discount_cents > 0 AND o.discount_code IS NOT NULL),
		    COALESCE(SUM(o.discount_cents) FILTER (WHERE ` + settledOrderSQL + ` AND o.discount_code IS NOT NULL), 0),
		    COUNT(*) FILTER (WHERE ` + settledOrderSQL + ` AND o.discount_cents > 0),
		    COALESCE(SUM(o.discount_cents) FILTER (WHERE ` + settledOrderSQL + `), 0)
		  FROM orders o
		  JOIN branches b ON b.id = o.branch_id
		 WHERE o.branch_id = $1
		   AND o.deleted_at IS NULL
		   AND (` + orderTimeSQL + ` AT TIME ZONE b.timezone)::date BETWEEN $2::date AND $3::date`

	var salesCount, cancelCount, returnOrderCount, taxCount, deliveryCount, couponCount, discountCount int
	var salesAmt, cancelAmt, returnOrderAmt, taxAmt, deliveryAmt, couponAmt, discountAmt int64
	if err := s.pool.QueryRow(ctx, q, branchID, from, to).Scan(
		&salesCount, &salesAmt, &cancelCount, &cancelAmt, &returnOrderCount, &returnOrderAmt,
		&taxCount, &taxAmt, &deliveryCount, &deliveryAmt, &couponCount, &couponAmt, &discountCount, &discountAmt); err != nil {
		return nil, err
	}

	/*
	 * Returns are counted by item, not by order: the count is how many units
	 * came back and the amount is what those units were sold for, summed
	 * across every order in the range whatever state the order ended in. An
	 * order-level count hid partial returns entirely and a full return was
	 * reported at the order total rather than the goods handed back.
	 */
	var returnQty int
	var returnAmt int64
	if err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(oi.returned_quantity), 0)::int,
		       COALESCE(SUM(oi.returned_quantity * oi.unit_price_cents), 0)::bigint
		  FROM order_items oi
		  JOIN orders o   ON o.id = oi.order_id
		  JOIN branches b ON b.id = o.branch_id
		 WHERE o.branch_id = $1
		   AND o.deleted_at IS NULL
		   AND COALESCE(oi.returned_quantity, 0) > 0
		   AND (`+orderTimeSQL+` AT TIME ZONE b.timezone)::date BETWEEN $2::date AND $3::date`,
		branchID, from, to).Scan(&returnQty, &returnAmt); err != nil {
		return nil, err
	}

	return []ordersapi.SummaryLine{
		{Label: "Sales", Count: salesCount, AmountCents: salesAmt},
		{Label: "Cancel Order", Count: cancelCount, AmountCents: cancelAmt},
		{Label: "Return Orders", Count: returnOrderCount, AmountCents: returnOrderAmt},
		{Label: "Return Items", Count: returnQty, AmountCents: returnAmt},
		{Label: "Tax", Count: taxCount, AmountCents: taxAmt},
		{Label: "Delivery Charge", Count: deliveryCount, AmountCents: deliveryAmt},
		{Label: "Loyalty/Coupons", Count: couponCount, AmountCents: couponAmt},
		{Label: "Discount", Count: discountCount, AmountCents: discountAmt},
	}, nil
}

func (s *Repo) TransactionSummaryForRange(ctx context.Context, branchID, from, to string) ([]ordersapi.SummaryLine, error) {
	var startCount int
	var startAmt int64
	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(SUM(starting_cash_cents), 0)
		  FROM pos_cash_days
		 WHERE branch_id = $1 AND business_date BETWEEN $2::date AND $3::date`,
		branchID, from, to).Scan(&startCount, &startAmt); err != nil {
		return nil, err
	}

	var payinCount, payoutCount int
	var payinAmt, payoutAmt int64
	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE kind = 'payin'),  COALESCE(SUM(amount_cents) FILTER (WHERE kind = 'payin'),  0),
		       COUNT(*) FILTER (WHERE kind = 'payout'), COALESCE(SUM(amount_cents) FILTER (WHERE kind = 'payout'), 0)
		  FROM pos_cash_movements
		 WHERE branch_id = $1 AND business_date BETWEEN $2::date AND $3::date`,
		branchID, from, to).Scan(&payinCount, &payinAmt, &payoutCount, &payoutAmt); err != nil {
		return nil, err
	}

	// Cash is still read so the query stays one round trip, but it is not a
	// line on the summary: the cash day (starting cash, pay-ins, pay-outs,
	// day close) accounts for cash on its own.
	var cashCount, cardCount int
	var cashAmt, cardAmt int64
	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE p.method = 'cash'), COALESCE(SUM(p.amount_cents) FILTER (WHERE p.method = 'cash'), 0),
		       COUNT(*) FILTER (WHERE p.method = 'card'), COALESCE(SUM(p.amount_cents) FILTER (WHERE p.method = 'card'), 0)
		  FROM bill_payments p
		  JOIN orders o   ON o.id = p.order_id
		  JOIN branches b ON b.id = o.branch_id
		 WHERE o.branch_id = $1
		   AND o.deleted_at IS NULL
		   -- Takings are payments on settled orders only. A payment against
		   -- an order that was then cancelled or returned is not money kept.
		   AND `+settledOrderSQL+`
		   AND (p.created_at AT TIME ZONE b.timezone)::date BETWEEN $2::date AND $3::date`,
		branchID, from, to).Scan(&cashCount, &cashAmt, &cardCount, &cardAmt); err != nil {
		return nil, err
	}

	var tipCount int
	var tipAmt int64
	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE o.tip_cents > 0), COALESCE(SUM(o.tip_cents), 0)
		  FROM orders o
		  JOIN branches b ON b.id = o.branch_id
		 WHERE o.branch_id = $1
		   AND o.deleted_at IS NULL
		   -- A tip is the staff's whatever became of the order, so settled,
		   -- cancelled and returned orders all count.
		   AND o.status IN ('fulfilled', 'cancelled', 'returned')
		   AND (`+orderTimeSQL+` AT TIME ZONE b.timezone)::date BETWEEN $2::date AND $3::date`,
		branchID, from, to).Scan(&tipCount, &tipAmt); err != nil {
		return nil, err
	}

	return []ordersapi.SummaryLine{
		{Label: "Starting Cash", Count: startCount, AmountCents: startAmt},
		{Label: "Card Payment", Count: cardCount, AmountCents: cardAmt},
		{Label: "Payins", Count: payinCount, AmountCents: payinAmt},
		{Label: "Payout", Count: payoutCount, AmountCents: payoutAmt},
		{Label: "Tip", Count: tipCount, AmountCents: tipAmt},
	}, nil
}

/*
 * HourlySalesForDay is the intraday chart: one row per hour of the branch's
 * own day, counting the same settled orders the day total counts.
 *
 * The rows span the branch's opening hours, but an hour with sales is shown
 * whether or not it falls inside them. Orders are stamped in the branch's
 * timezone, and a till used before opening, after closing, or on a branch
 * whose configured timezone is not where the staff actually are, used to
 * vanish from the chart while still counting in the day total — every hour
 * read 0 against a day that read 3.
 */
func (s *Repo) HourlySalesForDay(ctx context.Context, branchID, date string, openHour, closeHour int) ([]ordersapi.HourlyReportRow, error) {
	q := `
		SELECT EXTRACT(HOUR FROM (` + orderTimeSQL + ` AT TIME ZONE b.timezone))::int AS hr,
		       COUNT(*) FILTER (WHERE ` + settledOrderSQL + `),
		       COALESCE(SUM(o.total_cents) FILTER (WHERE ` + settledOrderSQL + `), 0)
		  FROM orders o
		  JOIN branches b ON b.id = o.branch_id
		 WHERE o.branch_id = $1
		   AND o.deleted_at IS NULL
		   AND (` + orderTimeSQL + ` AT TIME ZONE b.timezone)::date = $2::date
		 GROUP BY 1`

	rows, err := s.pool.Query(ctx, q, branchID, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byHour := map[int]ordersapi.HourlyReportRow{}
	for rows.Next() {
		var hr, count int
		var amt int64
		if err := rows.Scan(&hr, &count, &amt); err != nil {
			return nil, err
		}
		byHour[hr] = ordersapi.HourlyReportRow{Hour: hr, OrderCount: count, NetSalesCents: amt}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// The hours to show: the opening window, plus any hour outside it that
	// actually traded.
	show := map[int]bool{}
	h := openHour
	for i := 0; i < 24; i++ {
		show[h] = true
		if h == closeHour {
			break
		}
		h = (h + 1) % 24
	}
	for hr, row := range byHour {
		if row.OrderCount > 0 || row.NetSalesCents != 0 {
			show[hr] = true
		}
	}

	// Walk the clock once starting at opening so an overnight window
	// (22:00–02:00) still reads in order.
	out := []ordersapi.HourlyReportRow{}
	h = openHour
	for i := 0; i < 24; i++ {
		if show[h] {
			row, ok := byHour[h]
			if !ok {
				row = ordersapi.HourlyReportRow{Hour: h}
			}
			row.HourLabel = hourLabel12h(h)
			out = append(out, row)
		}
		h = (h + 1) % 24
	}
	return out, nil
}

// OrderCountOn counts the outlet's orders for a day, for the summary header.
func (s *Repo) OrderCountOn(ctx context.Context, branchID, date string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		  FROM orders o
		  JOIN branches b ON b.id = o.branch_id
		 WHERE o.branch_id = $1
		   AND o.deleted_at IS NULL
		   AND (`+orderTimeSQL+` AT TIME ZONE b.timezone)::date = $2::date`, branchID, date).Scan(&n)
	return n, err
}

func (s *Repo) OpenOrdersSummary(ctx context.Context, branchID string) (ordersapi.OpenOrdersSummary, error) {
	var out ordersapi.OpenOrdersSummary
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(SUM(total_cents), 0)
		  FROM orders
		 WHERE branch_id = $1
		   AND deleted_at IS NULL
		   AND status IN ('placed','confirmed','scheduled','in_production','ready','out_for_delivery')`,
		branchID).Scan(&out.Count, &out.AmountCents)
	return out, err
}

// Today returns the outlet's own date, which is the one the till means when
// it says "today".
func (s *Repo) BranchToday(ctx context.Context, branchID string) (string, error) {
	var d string
	err := s.pool.QueryRow(ctx, `
		SELECT to_char((NOW() AT TIME ZONE timezone)::date, 'YYYY-MM-DD')
		  FROM branches WHERE id = $1`, branchID).Scan(&d)
	if err == pgx.ErrNoRows {
		return time.Now().Format("2006-01-02"), nil
	}
	return d, err
}
