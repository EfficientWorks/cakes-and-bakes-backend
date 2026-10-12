package branchreport

import (
	"context"
	"fmt"
	"time"

	"bakehouse/services/admin/adminapi"
)

// ProductPerformance returns every product sold in the scope/period ordered
// by revenue descending — callers wanting "worst sellers" read from the
// tail of this same list rather than issuing a second query.
func (s *Repo) ProductPerformance(ctx context.Context, branchID string, from, to time.Time) ([]adminapi.ProductPerformance, error) {
	where := []string{"o.created_at >= $1", "o.created_at < $2", "o.status <> 'cancelled'"}
	args := []interface{}{from, to}
	if branchID != "" {
		args = append(args, branchID)
		where = append(where, fmt.Sprintf("o.branch_id = $%d", len(args)))
	}
	whereClause := where[0]
	for i := 1; i < len(where); i++ {
		whereClause += " AND " + where[i]
	}

	q := `
		SELECT COALESCE(oi.product_id::text, ''), oi.product_name, sum(oi.quantity), sum(oi.line_total_cents)
		FROM order_items oi JOIN (SELECT * FROM orders WHERE deleted_at IS NULL) o ON o.id = oi.order_id
		WHERE ` + whereClause + `
		GROUP BY oi.product_id, oi.product_name
		ORDER BY sum(oi.line_total_cents) DESC`

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []adminapi.ProductPerformance
	for rows.Next() {
		var p adminapi.ProductPerformance
		if err := rows.Scan(&p.ProductID, &p.ProductName, &p.QuantitySold, &p.RevenueCents); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// FulfilmentPerformance derives prep time and on-time completion from
// order_status_events — there is no separate fulfilment-tracking table.
// "On time" compares the first 'fulfilled' event against the order's
// scheduled slot end; this is a session-timezone approximation (branches
// carry their own IANA timezone, not applied here) accurate enough for a
// network-level performance signal.
func (s *Repo) FulfilmentPerformance(ctx context.Context, branchID string, from, to time.Time) (*adminapi.FulfilmentPerformance, error) {
	const q = `
		SELECT
			count(*) FILTER (WHERE fulfilled_evt.created_at IS NOT NULL) AS completed,
			coalesce(avg(EXTRACT(EPOCH FROM (ready_evt.created_at - o.created_at)) / 60)
				FILTER (WHERE ready_evt.created_at IS NOT NULL), 0) AS avg_prep_minutes,
			count(*) FILTER (WHERE fulfilled_evt.created_at IS NOT NULL
				AND fulfilled_evt.created_at <= (o.scheduled_date + o.slot_end)) AS on_time
		FROM (SELECT * FROM orders WHERE deleted_at IS NULL) o
		LEFT JOIN LATERAL (
			SELECT min(created_at) AS created_at FROM order_status_events
			WHERE order_id = o.id AND status = 'ready'
		) ready_evt ON true
		LEFT JOIN LATERAL (
			SELECT min(created_at) AS created_at FROM order_status_events
			WHERE order_id = o.id AND status = 'fulfilled'
		) fulfilled_evt ON true
		WHERE o.branch_id = $1 AND o.created_at >= $2 AND o.created_at < $3 AND o.status <> 'cancelled'`

	f := &adminapi.FulfilmentPerformance{BranchID: branchID}
	if err := s.pool.QueryRow(ctx, q, branchID, from, to).Scan(&f.CompletedOrders, &f.AvgPrepMinutes, &f.OnTimeCount); err != nil {
		return nil, err
	}
	if f.CompletedOrders > 0 {
		f.OnTimeRatePct = float64(f.OnTimeCount) / float64(f.CompletedOrders) * 100
	}
	return f, nil
}

// RefundCancellationStats is grouped by branch — pass a non-empty branchID
// to scope to one branch, or
// neither for the whole network.
func (s *Repo) RefundCancellationStats(ctx context.Context, branchID string, from, to time.Time) ([]adminapi.RefundCancellationStats, error) {
	where := []string{"o.created_at >= $1", "o.created_at < $2"}
	args := []interface{}{from, to}
	if branchID != "" {
		args = append(args, branchID)
		where = append(where, fmt.Sprintf("o.branch_id = $%d", len(args)))
	}
	whereClause := where[0]
	for i := 1; i < len(where); i++ {
		whereClause += " AND " + where[i]
	}

	// refunded_cents is joined via LATERAL (one aggregated row per order)
	// rather than a correlated subquery in the SELECT list — the latter
	// references o.id, a column outside the branch_id/name GROUP BY, which
	// Postgres rejects as an ungrouped-column reference even though the
	// subquery is otherwise valid.
	q := `
		SELECT o.branch_id, b.name,
			count(*) AS total_orders,
			count(*) FILTER (WHERE o.status = 'cancelled') AS cancelled_orders,
			count(*) FILTER (WHERE o.payment_status IN ('refunded','partial_refund')) AS refunded_orders,
			coalesce(sum(ref.amount_cents), 0) AS refunded_cents
		FROM (SELECT * FROM orders WHERE deleted_at IS NULL) o
		JOIN branches b ON b.id = o.branch_id
		LEFT JOIN LATERAL (
			SELECT sum(amount_cents) AS amount_cents FROM refunds r WHERE r.order_id = o.id AND r.status = 'succeeded'
		) ref ON true
		WHERE ` + whereClause + `
		GROUP BY o.branch_id, b.name
		ORDER BY b.name`

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []adminapi.RefundCancellationStats
	for rows.Next() {
		var r adminapi.RefundCancellationStats
		if err := rows.Scan(&r.BranchID, &r.BranchName, &r.TotalOrders, &r.CancelledOrders, &r.RefundedOrders, &r.RefundedCents); err != nil {
			return nil, err
		}
		if r.TotalOrders > 0 {
			r.CancellationPct = float64(r.CancelledOrders) / float64(r.TotalOrders) * 100
			r.RefundRatePct = float64(r.RefundedOrders) / float64(r.TotalOrders) * 100
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Repo) AmendmentLog(ctx context.Context, branchID string, from, to time.Time, page, limit int) ([]adminapi.AmendmentLogEntry, int, error) {
	if page < 1 {
		page = 1
	}
	if limit <= 0 || limit > 100 {
		limit = 25
	}

	var total int
	const countQ = `
		SELECT count(*) FROM order_amendments oa JOIN (SELECT * FROM orders WHERE deleted_at IS NULL) o ON o.id = oa.order_id
		WHERE o.branch_id = $1 AND oa.created_at >= $2 AND oa.created_at < $3`
	if err := s.pool.QueryRow(ctx, countQ, branchID, from, to).Scan(&total); err != nil {
		return nil, 0, err
	}

	const q = `
		SELECT oa.id, oa.order_id, o.reference, oa.actor_id, oa.actor_role, oa.reason,
			oa.before_value, oa.after_value, oa.price_delta_cents, oa.created_at
		FROM order_amendments oa JOIN (SELECT * FROM orders WHERE deleted_at IS NULL) o ON o.id = oa.order_id
		WHERE o.branch_id = $1 AND oa.created_at >= $2 AND oa.created_at < $3
		ORDER BY oa.created_at DESC
		LIMIT $4 OFFSET $5`
	rows, err := s.pool.Query(ctx, q, branchID, from, to, limit, (page-1)*limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []adminapi.AmendmentLogEntry
	for rows.Next() {
		var e adminapi.AmendmentLogEntry
		if err := rows.Scan(&e.ID, &e.OrderID, &e.OrderReference, &e.ActorID, &e.ActorRole, &e.Reason,
			&e.BeforeValue, &e.AfterValue, &e.PriceDeltaCents, &e.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}
