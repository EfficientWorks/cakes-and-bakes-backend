package network

import (
	"context"
	"fmt"
	"time"

	"bakehouse/services/admin/adminapi"
)

func (s *Repo) ChannelBreakdown(ctx context.Context, branchID string, from, to time.Time) ([]adminapi.ChannelBreakdown, error) {
	where := []string{"created_at >= $1", "created_at < $2", "status <> 'cancelled'"}
	args := []interface{}{from, to}
	if branchID != "" {
		args = append(args, branchID)
		where = append(where, fmt.Sprintf("branch_id = $%d", len(args)))
	}
	whereClause := where[0]
	for i := 1; i < len(where); i++ {
		whereClause += " AND " + where[i]
	}

	q := `
		SELECT channel, count(*), coalesce(sum(total_cents),0)
		FROM (SELECT * FROM orders WHERE deleted_at IS NULL) orders WHERE channel <> 'phone' AND ` + whereClause + `
		GROUP BY channel ORDER BY count(*) DESC`

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []adminapi.ChannelBreakdown
	for rows.Next() {
		var c adminapi.ChannelBreakdown
		if err := rows.Scan(&c.Channel, &c.OrderCount, &c.RevenueCents); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Repo) PeakPeriods(ctx context.Context, branchID string, from, to time.Time) ([]adminapi.PeakPeriodCell, error) {
	where := []string{"created_at >= $1", "created_at < $2", "status <> 'cancelled'"}
	args := []interface{}{from, to}
	if branchID != "" {
		args = append(args, branchID)
		where = append(where, fmt.Sprintf("branch_id = $%d", len(args)))
	}
	whereClause := where[0]
	for i := 1; i < len(where); i++ {
		whereClause += " AND " + where[i]
	}

	q := `
		SELECT EXTRACT(DOW FROM created_at)::int, EXTRACT(HOUR FROM created_at)::int, count(*), coalesce(sum(total_cents),0)
		FROM (SELECT * FROM orders WHERE deleted_at IS NULL) orders WHERE ` + whereClause + `
		GROUP BY 1, 2 ORDER BY 1, 2`

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []adminapi.PeakPeriodCell
	for rows.Next() {
		var c adminapi.PeakPeriodCell
		if err := rows.Scan(&c.DayOfWeek, &c.Hour, &c.OrderCount, &c.RevenueCents); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

/*
 * How orders were handed over.
 *
 * The same shape as ChannelBreakdown and deliberately so: the reports screen
 * shows them side by side, and two breakdowns that counted differently would
 * be read as though they did not.
 *
 * Every type is returned even at zero. A shop that did no deliveries today
 * should show "Delivery 0", not drop the row and leave the reader wondering
 * whether it was nil or nobody asked.
 */
func (s *Repo) FulfilmentBreakdown(ctx context.Context, branchID string, from, to time.Time) ([]adminapi.FulfilmentBreakdown, error) {
	args := []any{from, to}
	where := []string{"created_at >= $1", "created_at < $2"}
	if branchID != "" {
		args = append(args, branchID)
		where = append(where, fmt.Sprintf("branch_id = $%d", len(args)))
	}
	whereClause := where[0]
	for i := 1; i < len(where); i++ {
		whereClause += " AND " + where[i]
	}

	q := `
		SELECT t.fulfilment_type,
		       count(o.id),
		       coalesce(sum(o.total_cents), 0)
		  FROM (VALUES ('pickup'), ('delivery'), ('takeaway')) AS t(fulfilment_type)
		  LEFT JOIN (SELECT * FROM orders WHERE deleted_at IS NULL AND ` + whereClause + `) o
		         ON o.fulfilment_type = t.fulfilment_type
		 GROUP BY t.fulfilment_type
		 ORDER BY count(o.id) DESC`

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []adminapi.FulfilmentBreakdown{}
	for rows.Next() {
		var f adminapi.FulfilmentBreakdown
		if err := rows.Scan(&f.FulfilmentType, &f.OrderCount, &f.RevenueCents); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
