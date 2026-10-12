package network

import (
	"context"
	"fmt"
	"strings"
	"time"

	"bakehouse/pkg/tenant"
	"bakehouse/services/admin/adminapi"
)

func (s *Repo) NetworkDashboard(ctx context.Context, today time.Time, branchID string) (*adminapi.NetworkDashboard, error) {
	d := &adminapi.NetworkDashboard{Pipeline: map[string]int{}}

	// scope narrows every figure on the dashboard — totals, the live pipeline
	// (in production / ready / out for delivery) and the league — to one
	// branch. Empty means "the whole network", which is
	// what the dashboard always showed before the filters existed — of the
	// caller's own business only.
	scope := func(alias string, args []any) (string, []any) {
		clause := ""
		if tenant.Scoped(ctx) {
			args = append(args, tenant.BusinessParam(ctx))
			clause += fmt.Sprintf(" AND %sbusiness_id IS NOT DISTINCT FROM $%d", alias, len(args))
		}
		if branchID != "" {
			args = append(args, branchID)
			clause += fmt.Sprintf(" AND %sbranch_id = $%d", alias, len(args))
		}
		return clause, args
	}

	totalsWhere, totalsArgs := scope("", []any{today})
	totalsQ := `
		SELECT count(*), coalesce(sum(total_cents),0)
		FROM (SELECT * FROM orders WHERE deleted_at IS NULL) orders
		WHERE created_at::date = $1::date AND status <> 'cancelled'` + totalsWhere
	if err := s.pool.QueryRow(ctx, totalsQ, totalsArgs...).Scan(&d.TodayOrders, &d.TodayRevenueCents); err != nil {
		return nil, err
	}
	if d.TodayOrders > 0 {
		d.TodayAOVCents = d.TodayRevenueCents / int64(d.TodayOrders)
	}

	pipelineWhere, pipelineArgs := scope("", nil)
	pipelineQ := `
		SELECT status, count(*) FROM (SELECT * FROM orders WHERE deleted_at IS NULL) orders
		WHERE status IN ('confirmed','scheduled','in_production','ready','out_for_delivery')` + pipelineWhere + `
		GROUP BY status`
	rows, err := s.pool.Query(ctx, pipelineQ, pipelineArgs...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			rows.Close()
			return nil, err
		}
		d.Pipeline[status] = count
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// The league is a list of branches, so it is scoped on the branch row
	// itself: a branch filter leaves one row.
	leagueArgs := []any{today}
	leagueWhere := ""
	if tenant.Scoped(ctx) {
		leagueArgs = append(leagueArgs, tenant.BusinessParam(ctx))
		leagueWhere += fmt.Sprintf(" AND b.business_id IS NOT DISTINCT FROM $%d", len(leagueArgs))
	}
	if branchID != "" {
		leagueArgs = append(leagueArgs, branchID)
		leagueWhere += fmt.Sprintf(" AND b.id = $%d", len(leagueArgs))
	}
	leagueQ := `
		SELECT b.id, b.name, count(o.id) FILTER (WHERE o.id IS NOT NULL AND o.status <> 'cancelled'),
			coalesce(sum(o.total_cents) FILTER (WHERE o.status <> 'cancelled'), 0)
		FROM branches b
		LEFT JOIN (SELECT * FROM orders WHERE deleted_at IS NULL) o ON o.branch_id = b.id AND o.created_at::date = $1::date
		WHERE true` + leagueWhere + `
		GROUP BY b.id, b.name
		ORDER BY coalesce(sum(o.total_cents) FILTER (WHERE o.status <> 'cancelled'), 0) DESC
		LIMIT 25`
	leagueRows, err := s.pool.Query(ctx, leagueQ, leagueArgs...)
	if err != nil {
		return nil, err
	}
	defer leagueRows.Close()
	for leagueRows.Next() {
		var row adminapi.BranchLeagueRow
		if err := leagueRows.Scan(&row.BranchID, &row.BranchName, &row.OrderCount, &row.RevenueCents); err != nil {
			return nil, err
		}
		d.BranchLeague = append(d.BranchLeague, row)
	}
	return d, leagueRows.Err()
}

// RevenueTrend buckets orders by date_trunc(unit, created_at); unit must
// already be validated by the caller against {day,week,month} — it is
// passed as a bind parameter, never interpolated into the query.
func (s *Repo) RevenueTrend(ctx context.Context, branchID, unit string, from, to time.Time) ([]adminapi.RevenueTrendPoint, error) {
	where := []string{"created_at >= $2", "created_at < $3", "status <> 'cancelled'"}
	args := []interface{}{unit, from, to}
	if tenant.Scoped(ctx) {
		args = append(args, tenant.BusinessParam(ctx))
		where = append(where, fmt.Sprintf("business_id IS NOT DISTINCT FROM $%d", len(args)))
	}
	if branchID != "" {
		args = append(args, branchID)
		where = append(where, fmt.Sprintf("branch_id = $%d", len(args)))
	}
	whereClause := where[0]
	for i := 1; i < len(where); i++ {
		whereClause += " AND " + where[i]
	}

	// Zero-filled: every bucket between from and to appears, including the
	// ones with no orders, so a chart draws a continuous line. $3 is the
	// exclusive upper bound, so the series stops just before it.
	q := `
		WITH buckets AS (
			SELECT generate_series(
				date_trunc($1, $2::timestamptz),
				date_trunc($1, $3::timestamptz - interval '1 microsecond'),
				('1 ' || $1)::interval
			) AS period
		), agg AS (
			SELECT date_trunc($1, created_at) AS period, count(*) AS n, coalesce(sum(total_cents),0) AS cents
			FROM (SELECT * FROM orders WHERE deleted_at IS NULL) orders
			WHERE ` + whereClause + `
			GROUP BY 1
		)
		SELECT b.period, coalesce(a.n, 0), coalesce(a.cents, 0)
		FROM buckets b LEFT JOIN agg a ON a.period = b.period
		ORDER BY b.period`

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []adminapi.RevenueTrendPoint
	for rows.Next() {
		var period time.Time
		var p adminapi.RevenueTrendPoint
		if err := rows.Scan(&period, &p.OrderCount, &p.RevenueCents); err != nil {
			return nil, err
		}
		p.Period = period.Format("2006-01-02")
		out = append(out, p)
	}
	return out, rows.Err()
}

// RevenueByBranch is each branch's order count and takings over [from, to),
// same filters as RevenueTrend. Every branch appears (zero when it sold
// nothing) unless branchID narrows it to one. Highest revenue first.
func (s *Repo) RevenueByBranch(ctx context.Context, branchID string, from, to time.Time) ([]adminapi.RevenueTrendBranch, error) {
	args := []interface{}{from, to}
	conds := []string{}
	// "Every branch" means every branch of the caller's business.
	if tenant.Scoped(ctx) {
		args = append(args, tenant.BusinessParam(ctx))
		conds = append(conds, fmt.Sprintf("b.business_id IS NOT DISTINCT FROM $%d", len(args)))
	}
	if branchID != "" {
		args = append(args, branchID)
		conds = append(conds, fmt.Sprintf("b.id = $%d", len(args)))
	}
	branchWhere := ""
	if len(conds) > 0 {
		branchWhere = " WHERE " + strings.Join(conds, " AND ")
	}
	// An explicitly chosen branch always comes back, even with no sales.
	keepOne := ""
	if branchID != "" {
		keepOne = " OR true"
	}
	q := `
		SELECT b.id, b.name, count(o.id), coalesce(sum(o.total_cents), 0)
		FROM branches b
		LEFT JOIN orders o ON o.branch_id = b.id
			AND o.deleted_at IS NULL
			AND o.status <> 'cancelled'
			AND o.created_at >= $1 AND o.created_at < $2` + branchWhere + `
		GROUP BY b.id, b.name, b.status
		-- A closed or pending branch with no sales in range is noise; one
		-- that did sell still counts toward the total, so it stays.
		HAVING count(o.id) > 0 OR b.status = 'active'` + keepOne + `
		ORDER BY 4 DESC, b.name`

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []adminapi.RevenueTrendBranch{}
	for rows.Next() {
		var r adminapi.RevenueTrendBranch
		if err := rows.Scan(&r.BranchID, &r.BranchName, &r.OrderCount, &r.RevenueCents); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// OutletsToday returns today's order count and takings for every branch the
// caller may see.
//
// One query rather than one dashboard call per outlet: the branch directory
// shows these two numbers per row, and a network of thirty outlets should not
// mean thirty round trips to draw one table.
//
// Branches with no orders today are returned with zeroes rather than omitted,
// so the caller can render every row without deciding what a missing branch
// means. Cancelled orders are excluded, matching every other revenue figure
// in this service.
func (s *Repo) OutletsToday(ctx context.Context, day time.Time, branchID string) ([]adminapi.BranchLeagueRow, error) {
	const q = `
		SELECT b.id, b.name,
		       count(o.id) FILTER (WHERE o.status <> 'cancelled'),
		       coalesce(sum(o.total_cents) FILTER (WHERE o.status <> 'cancelled'), 0)
		  FROM branches b
		  LEFT JOIN (SELECT * FROM orders WHERE deleted_at IS NULL) o
		         ON o.branch_id = b.id
		        AND o.created_at::date = $1::date
		 WHERE ($2 = '' OR b.id = $2::uuid)
		   AND (NOT $3::boolean OR b.business_id IS NOT DISTINCT FROM $4::uuid)
		 GROUP BY b.id, b.name
		 ORDER BY b.name`

	// $3/$4 keep a brand-wide caller to their own business's outlets.
	rows, err := s.pool.Query(ctx, q, day, branchID, tenant.Scoped(ctx), tenant.BusinessParam(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []adminapi.BranchLeagueRow{}
	for rows.Next() {
		var row adminapi.BranchLeagueRow
		if err := rows.Scan(&row.BranchID, &row.BranchName, &row.OrderCount, &row.RevenueCents); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
