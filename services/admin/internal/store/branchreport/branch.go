package branchreport

import (
	"context"
	"time"

	"bakehouse/services/admin/adminapi"
)

// BranchInfoByID returns just enough of a branch to enforce scope and label
// reports — used instead of depending on the tenancy package directly.
func (s *Repo) BranchInfoByID(ctx context.Context, branchID string) (*adminapi.BranchInfo, error) {
	const q = `SELECT id, name, status, created_at FROM branches WHERE id = $1`
	var b adminapi.BranchInfo
	err := s.pool.QueryRow(ctx, q, branchID).Scan(&b.ID, &b.Name, &b.Status, &b.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (s *Repo) BranchDashboard(ctx context.Context, branchID string, today time.Time) (*adminapi.BranchDashboard, error) {
	d := &adminapi.BranchDashboard{BranchID: branchID, StatusCounts: map[string]int{}}

	const totalsQ = `
		SELECT count(*), coalesce(sum(total_cents),0)
		FROM (SELECT * FROM orders WHERE deleted_at IS NULL) orders WHERE branch_id = $1 AND created_at::date = $2::date AND status <> 'cancelled'`
	if err := s.pool.QueryRow(ctx, totalsQ, branchID, today).Scan(&d.TodayOrders, &d.TodayRevenueCents); err != nil {
		return nil, err
	}
	if d.TodayOrders > 0 {
		d.TodayAOVCents = d.TodayRevenueCents / int64(d.TodayOrders)
	}

	const statusQ = `SELECT status, count(*) FROM (SELECT * FROM orders WHERE deleted_at IS NULL) orders WHERE branch_id = $1 AND created_at::date = $2::date GROUP BY status`
	rows, err := s.pool.Query(ctx, statusQ, branchID, today)
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
		d.StatusCounts[status] = count
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	const slotQ = `
		SELECT date, slot_start, fulfilment_type, max_capacity, reserved_count
		FROM slot_capacity
		WHERE branch_id = $1 AND date >= $2::date AND date <= ($2::date + 7)
		ORDER BY date, slot_start`
	slotRows, err := s.pool.Query(ctx, slotQ, branchID, today)
	if err != nil {
		return nil, err
	}
	defer slotRows.Close()
	for slotRows.Next() {
		var sl adminapi.SlotLoad
		var date time.Time
		var slotStart time.Time
		if err := slotRows.Scan(&date, &slotStart, &sl.FulfilmentType, &sl.MaxCapacity, &sl.ReservedCount); err != nil {
			return nil, err
		}
		sl.Date = date.Format("2006-01-02")
		sl.SlotStart = slotStart.Format("15:04")
		d.UpcomingSlotLoad = append(d.UpcomingSlotLoad, sl)
	}
	return d, slotRows.Err()
}

func (s *Repo) SalesByDay(ctx context.Context, branchID string, from, to time.Time) ([]adminapi.SalesPoint, error) {
	const q = `
		SELECT created_at::date AS d, count(*), coalesce(sum(total_cents),0)
		FROM (SELECT * FROM orders WHERE deleted_at IS NULL) orders
		WHERE branch_id = $1 AND created_at >= $2 AND created_at < $3 AND status <> 'cancelled'
		GROUP BY d ORDER BY d`
	rows, err := s.pool.Query(ctx, q, branchID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []adminapi.SalesPoint
	for rows.Next() {
		var d time.Time
		var p adminapi.SalesPoint
		if err := rows.Scan(&d, &p.OrderCount, &p.RevenueCents); err != nil {
			return nil, err
		}
		p.Date = d.Format("2006-01-02")
		out = append(out, p)
	}
	return out, rows.Err()
}
