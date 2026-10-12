package cash

import (
	"context"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/orders/internal/store/shared"
	"bakehouse/services/orders/ordersapi"
)

// AddCashMovement records money into or out of the drawer.
func (s *Repo) AddCashMovement(ctx context.Context, in ordersapi.CashMovementInput, createdBy string) (*ordersapi.CashMovement, error) {
	var m ordersapi.CashMovement
	err := s.pool.QueryRow(ctx, `
		INSERT INTO pos_cash_movements (branch_id, business_date, kind, amount_cents, reason, created_by)
		VALUES ($1,$2::date,$3,$4,$5,$6)
		RETURNING id, branch_id, to_char(business_date, 'YYYY-MM-DD'), kind, amount_cents, reason,
		          COALESCE(created_by::text, ''), created_at`,
		in.BranchID, in.BusinessDate, in.Kind, in.AmountCents, in.Reason, shared.NilIfEmptyStr(createdBy)).
		Scan(&m.ID, &m.BranchID, &m.BusinessDate, &m.Kind, &m.AmountCents, &m.Reason, &m.CreatedBy, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// ListCashMovements returns a day's pay-ins and pay-outs, oldest first.
func (s *Repo) ListCashMovements(ctx context.Context, branchID, date string) ([]ordersapi.CashMovement, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, branch_id, to_char(business_date, 'YYYY-MM-DD'), kind, amount_cents, reason,
		       COALESCE(created_by::text, ''), created_at
		  FROM pos_cash_movements
		 WHERE branch_id = $1 AND business_date = $2::date
		 ORDER BY created_at`, branchID, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ordersapi.CashMovement{}
	for rows.Next() {
		var m ordersapi.CashMovement
		if err := rows.Scan(&m.ID, &m.BranchID, &m.BusinessDate, &m.Kind, &m.AmountCents,
			&m.Reason, &m.CreatedBy, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetCashMovement reads one movement, so the caller can check its outlet
// before deleting it.
func (s *Repo) GetCashMovement(ctx context.Context, id string) (*ordersapi.CashMovement, error) {
	var m ordersapi.CashMovement
	err := s.pool.QueryRow(ctx, `
		SELECT id, branch_id, to_char(business_date, 'YYYY-MM-DD'), kind, amount_cents, reason,
		       COALESCE(created_by::text, ''), created_at
		  FROM pos_cash_movements WHERE id = $1`, id).
		Scan(&m.ID, &m.BranchID, &m.BusinessDate, &m.Kind, &m.AmountCents, &m.Reason, &m.CreatedBy, &m.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, ordersapi.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// DeleteCashMovement removes a mistaken entry.
func (s *Repo) DeleteCashMovement(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM pos_cash_movements WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ordersapi.ErrNotFound
	}
	return nil
}

// CashMovementTotals returns the day's pay-in and pay-out sums.
func (s *Repo) CashMovementTotals(ctx context.Context, branchID, date string) (payIns, payOuts int64, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount_cents) FILTER (WHERE kind = 'payin'),  0),
		       COALESCE(SUM(amount_cents) FILTER (WHERE kind = 'payout'), 0)
		  FROM pos_cash_movements
		 WHERE branch_id = $1 AND business_date = $2::date`, branchID, date).Scan(&payIns, &payOuts)
	return payIns, payOuts, err
}

/*
 * TenderTotals is what was taken at an outlet on one day, split by method.
 *
 * Read from bill_payments rather than from order totals: an order's total is
 * what it came to, while a payment is money that actually changed hands. A
 * drawer is reconciled against the second.
 */
func (s *Repo) TenderTotals(ctx context.Context, branchID, date string) ([]ordersapi.TenderTotal, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.method, COALESCE(SUM(p.amount_cents), 0), COUNT(*)
		  FROM bill_payments p
		  JOIN orders o   ON o.id = p.order_id
		  JOIN branches b ON b.id = o.branch_id
		 WHERE o.branch_id = $1
		   AND (p.created_at AT TIME ZONE b.timezone)::date = $2::date
		 GROUP BY p.method
		 ORDER BY p.method`, branchID, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ordersapi.TenderTotal{}
	for rows.Next() {
		var t ordersapi.TenderTotal
		if err := rows.Scan(&t.Method, &t.AmountCents, &t.Count); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
