package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"bakehouse/services/payments/paymentsapi"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) CreatePayment(ctx context.Context, p paymentsapi.Payment) (*paymentsapi.Payment, error) {
	const q = `
		INSERT INTO payments (order_id, branch_id, method, amount_cents, currency,
			provider_payment_id, provider_payment_method_id, status, failure_reason, captured_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING id, created_at`
	err := s.pool.QueryRow(ctx, q, p.OrderID, p.BranchID, p.Method, p.AmountCents, p.Currency,
		p.ProviderPaymentID, p.ProviderPaymentMethodID, p.Status, p.FailureReason, p.CapturedAt).
		Scan(&p.ID, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

const paymentSelect = `
	SELECT id, order_id, branch_id, method, amount_cents, currency,
	       provider_payment_id, provider_payment_method_id, status, failure_reason, captured_at, created_at
	FROM payments`

func scanPayment(row pgx.Row) (*paymentsapi.Payment, error) {
	var p paymentsapi.Payment
	err := row.Scan(&p.ID, &p.OrderID, &p.BranchID, &p.Method, &p.AmountCents, &p.Currency,
		&p.ProviderPaymentID, &p.ProviderPaymentMethodID, &p.Status, &p.FailureReason, &p.CapturedAt, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, err
}

func (s *Store) GetPaymentByProviderID(ctx context.Context, paymentID string) (*paymentsapi.Payment, error) {
	return scanPayment(s.pool.QueryRow(ctx, paymentSelect+" WHERE provider_payment_id = $1", paymentID))
}

func (s *Store) GetPaymentByOrderID(ctx context.Context, orderID string) (*paymentsapi.Payment, error) {
	return scanPayment(s.pool.QueryRow(ctx, paymentSelect+" WHERE order_id = $1 ORDER BY created_at DESC LIMIT 1", orderID))
}

func (s *Store) LinkPaymentToOrder(ctx context.Context, providerPaymentID, orderID, branchID string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE payments SET order_id = $2, branch_id = $3 WHERE provider_payment_id = $1`,
		providerPaymentID, orderID, branchID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdatePaymentStatus(ctx context.Context, providerPaymentID, status string, failureReason *string) error {
	_, err := s.pool.Exec(ctx, `UPDATE payments SET status = $2, failure_reason = $3 WHERE provider_payment_id = $1`, providerPaymentID, status, failureReason)
	return err
}

func (s *Store) UpdatePaymentStatusByOrder(ctx context.Context, orderID, status string) error {
	_, err := s.pool.Exec(ctx, `UPDATE payments SET status = $2 WHERE order_id = $1`, orderID, status)
	return err
}

// --- Refunds -----------------------------------------------------------------

func (s *Store) CreateRefund(ctx context.Context, r paymentsapi.Refund) (*paymentsapi.Refund, error) {
	const q = `
		INSERT INTO refunds (payment_id, order_id, amount_cents, reason, provider_refund_id, status, issued_by_user_id, approved_by_user_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING id, created_at`
	err := s.pool.QueryRow(ctx, q, r.PaymentID, r.OrderID, r.AmountCents, r.Reason, r.ProviderRefundID, r.Status, r.IssuedByUserID, r.ApprovedByUserID).
		Scan(&r.ID, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *Store) ListRefundsByOrder(ctx context.Context, orderID string) ([]paymentsapi.Refund, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, payment_id, order_id, amount_cents, reason, provider_refund_id, status, issued_by_user_id, approved_by_user_id, created_at
		FROM refunds WHERE order_id = $1 ORDER BY created_at`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []paymentsapi.Refund
	for rows.Next() {
		var r paymentsapi.Refund
		if err := rows.Scan(&r.ID, &r.PaymentID, &r.OrderID, &r.AmountCents, &r.Reason, &r.ProviderRefundID, &r.Status, &r.IssuedByUserID, &r.ApprovedByUserID, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetRefundStatus moves a refund to a status; false when it already had it
// (webhooks repeat) or is unknown here.
func (s *Store) SetRefundStatus(ctx context.Context, refundID, status string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE refunds SET status = $2 WHERE provider_refund_id = $1 AND status <> $2`, refundID, status)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
