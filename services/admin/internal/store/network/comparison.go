package network

import (
	"context"
	"fmt"
	"time"

	"bakehouse/pkg/tenant"
	"bakehouse/services/admin/adminapi"
)

func (s *Repo) ListBranchesForComparison(ctx context.Context) ([]adminapi.BranchInfo, error) {
	q := `SELECT id, name, status, created_at FROM branches`
	args := []any{}
	// The network being compared is the caller's own business.
	if tenant.Scoped(ctx) {
		args = append(args, tenant.BusinessParam(ctx))
		q += fmt.Sprintf(" WHERE business_id IS NOT DISTINCT FROM $%d", len(args))
	}
	q += ` ORDER BY name`

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []adminapi.BranchInfo
	for rows.Next() {
		var b adminapi.BranchInfo
		if err := rows.Scan(&b.ID, &b.Name, &b.Status, &b.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// OrdersAggByBranch returns each branch's revenue/order totals for the
// current and comparison periods in one pass, keyed by branch ID.
func (s *Repo) OrdersAggByBranch(ctx context.Context, currentFrom, currentTo, compareFrom, compareTo time.Time) (map[string]adminapi.BranchOrderAgg, error) {
	where := []string{"status <> 'cancelled'"}
	args := []interface{}{currentFrom, currentTo, compareFrom, compareTo}
	if tenant.Scoped(ctx) {
		args = append(args, tenant.BusinessParam(ctx))
		where = append(where, fmt.Sprintf("business_id IS NOT DISTINCT FROM $%d", len(args)))
	}
	whereClause := where[0]
	for i := 1; i < len(where); i++ {
		whereClause += " AND " + where[i]
	}

	q := `
		SELECT branch_id,
			coalesce(sum(total_cents) FILTER (WHERE created_at >= $1 AND created_at < $2), 0),
			count(*) FILTER (WHERE created_at >= $1 AND created_at < $2),
			coalesce(sum(total_cents) FILTER (WHERE created_at >= $3 AND created_at < $4), 0),
			count(*) FILTER (WHERE created_at >= $3 AND created_at < $4)
		FROM (SELECT * FROM orders WHERE deleted_at IS NULL) orders
		WHERE ` + whereClause + `
		GROUP BY branch_id`

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]adminapi.BranchOrderAgg{}
	for rows.Next() {
		var branchID string
		var agg adminapi.BranchOrderAgg
		if err := rows.Scan(&branchID, &agg.CurrentRevenueCents, &agg.CurrentOrders, &agg.CompareRevenueCents, &agg.CompareOrders); err != nil {
			return nil, err
		}
		out[branchID] = agg
	}
	return out, rows.Err()
}

// BranchPerformance totals each outlet's orders over a window.
//
// Cancelled orders are left out of both figures: they are not trade, and
// counting them would make a bad day look like a busy one.
func (s *Repo) BranchPerformance(ctx context.Context, from, to time.Time) ([]adminapi.BranchPerformance, error) {
	args := []any{from, to}
	scope := ""
	// Every outlet of the caller's business, and only those.
	if tenant.Scoped(ctx) {
		args = append(args, tenant.BusinessParam(ctx))
		scope = fmt.Sprintf(" WHERE b.business_id IS NOT DISTINCT FROM $%d", len(args))
	}
	q := `
		SELECT b.id, b.name, b.ownership_type,
		       count(o.id),
		       coalesce(sum(o.total_cents), 0)
		  FROM branches b
		  LEFT JOIN orders o
		         ON o.branch_id = b.id
		        AND o.deleted_at IS NULL
		        AND o.status <> 'cancelled'
		        AND o.created_at >= $1 AND o.created_at < $2` + scope + `
		 GROUP BY b.id, b.name, b.ownership_type
		 ORDER BY coalesce(sum(o.total_cents), 0) DESC`

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []adminapi.BranchPerformance{}
	for rows.Next() {
		var p adminapi.BranchPerformance
		if err := rows.Scan(&p.BranchID, &p.BranchName, &p.OwnershipType, &p.OrderCount, &p.RevenueCents); err != nil {
			return nil, err
		}
		if p.OrderCount > 0 {
			p.AverageOrderValueCents = p.RevenueCents / int64(p.OrderCount)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
