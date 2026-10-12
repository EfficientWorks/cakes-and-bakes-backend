package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/payments/paymentsapi"
)

// --- Payment links ------------------------------------------------------------

const linkSelect = `
	SELECT id, order_id, bill_id, branch_id, provider, provider_session_id, url, amount_cents, currency,
	       customer_email, status, payment_ref, sent_count, created_at, paid_at
	  FROM payment_links`

func scanLink(row pgx.Row) (*paymentsapi.PaymentLink, error) {
	var l paymentsapi.PaymentLink
	err := row.Scan(&l.ID, &l.OrderID, &l.BillID, &l.BranchID, &l.Provider, &l.SessionID, &l.URL, &l.AmountCents,
		&l.Currency, &l.CustomerEmail, &l.Status, &l.PaymentRef, &l.SentCount, &l.CreatedAt, &l.PaidAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &l, err
}

// LinkRow is a payment link with what only the store keeps.
type LinkRow struct {
	paymentsapi.PaymentLink
	CreatedBy *string
}

// CreateLink saves a link Stripe has just opened. The partial unique index
// refuses a second open link for the same order.
func (s *Store) CreateLink(ctx context.Context, l LinkRow) (*paymentsapi.PaymentLink, error) {
	var id string
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO payment_links (order_id, bill_id, branch_id, provider, provider_session_id,
			url, amount_cents, currency, customer_email, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING id`,
		l.OrderID, l.BillID, l.BranchID, l.Provider, l.SessionID,
		l.URL, l.AmountCents, l.Currency, l.CustomerEmail, l.CreatedBy).Scan(&id); err != nil {
		return nil, err
	}
	return s.GetLink(ctx, id)
}

func (s *Store) GetLink(ctx context.Context, id string) (*paymentsapi.PaymentLink, error) {
	return scanLink(s.pool.QueryRow(ctx, linkSelect+` WHERE id = $1`, id))
}

func (s *Store) GetOpenLinkByOrder(ctx context.Context, orderID string) (*paymentsapi.PaymentLink, error) {
	return scanLink(s.pool.QueryRow(ctx, linkSelect+` WHERE order_id = $1 AND status = 'open'`, orderID))
}

func (s *Store) GetLatestLinkByOrder(ctx context.Context, orderID string) (*paymentsapi.PaymentLink, error) {
	return scanLink(s.pool.QueryRow(ctx, linkSelect+` WHERE order_id = $1 ORDER BY created_at DESC LIMIT 1`, orderID))
}

func (s *Store) GetLinkBySession(ctx context.Context, provider, sessionID string) (*paymentsapi.PaymentLink, error) {
	return scanLink(s.pool.QueryRow(ctx, linkSelect+` WHERE provider = $1 AND provider_session_id = $2`, provider, sessionID))
}

// MarkLinkPaid records the payment that paid a link. Only the first call wins
// (a webhook can arrive twice): false means it was already recorded. A link
// cancelled or expired a moment before the customer paid is still marked
// paid — the money moved, and it has to be counted.
func (s *Store) MarkLinkPaid(ctx context.Context, id, paymentRef string, at time.Time) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE payment_links SET status = 'paid', payment_ref = $2, paid_at = $3, updated_at = NOW()
		 WHERE id = $1 AND payment_ref IS NULL`, id, paymentRef, at)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// CloseLink moves an open link to expired or cancelled. False: it was not open.
func (s *Store) CloseLink(ctx context.Context, id, status string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE payment_links SET status = $2, updated_at = NOW()
		 WHERE id = $1 AND status = 'open'`, id, status)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Store) IncrementLinkSent(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `UPDATE payment_links SET sent_count = sent_count + 1, updated_at = NOW() WHERE id = $1`, id)
	return err
}

// --- Refund sources -----------------------------------------------------------

// RefundSource is one Stripe payment on an order and what it can still give back.
type RefundSource struct {
	PaymentID      string
	PaymentRef     string
	RemainingCents int64
	Currency       string
}

// RefundSources lists an order's Stripe payments that can still be refunded,
// largest remaining first. A failed refund gave nothing back; a pending one is
// counted as given (it may yet settle).
func (s *Store) RefundSources(ctx context.Context, orderID string) ([]RefundSource, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.id, p.provider_payment_id, p.currency,
		       p.amount_cents - COALESCE(SUM(r.amount_cents) FILTER (WHERE r.status <> 'failed'), 0) AS remaining
		  FROM payments p
		  LEFT JOIN refunds r ON r.payment_id = p.id
		 WHERE p.order_id = $1
		   AND p.provider_payment_id LIKE 'pi\_%'
		   AND p.status IN ('captured', 'partial_refund', 'refunded')
		 GROUP BY p.id, p.provider_payment_id, p.currency, p.amount_cents
		HAVING p.amount_cents - COALESCE(SUM(r.amount_cents) FILTER (WHERE r.status <> 'failed'), 0) > 0
		 ORDER BY remaining DESC, p.created_at`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RefundSource
	for rows.Next() {
		var r RefundSource
		if err := rows.Scan(&r.PaymentID, &r.PaymentRef, &r.Currency, &r.RemainingCents); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// EnsureCapturedPayment writes the ledger row for a Stripe payment if it has
// none, and links it to the order. Used for payments taken through a link and
// to backfill website orders paid before the ledger was linked.
func (s *Store) EnsureCapturedPayment(ctx context.Context, paymentRef, orderID, branchID string, amountCents int64, currency string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO payments (order_id, branch_id, method, amount_cents, currency, provider_payment_id, status, captured_at)
		VALUES ($2, $3, 'card', $4, $5, $1, 'captured', NOW())
		ON CONFLICT (provider_payment_id) WHERE provider_payment_id IS NOT NULL
		DO UPDATE SET order_id = COALESCE(payments.order_id, EXCLUDED.order_id),
		              branch_id = COALESCE(payments.branch_id, EXCLUDED.branch_id)`,
		paymentRef, orderID, branchID, amountCents, currency)
	return err
}

// SyncPaymentRefundStatus sets a payment to partial_refund or refunded from
// what its refunds add up to.
func (s *Store) SyncPaymentRefundStatus(ctx context.Context, paymentRef string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE payments p
		   SET status = CASE WHEN given.total >= p.amount_cents THEN 'refunded'
		                     WHEN given.total > 0 THEN 'partial_refund'
		                     ELSE p.status END
		  FROM (SELECT COALESCE(SUM(r.amount_cents) FILTER (WHERE r.status <> 'failed'), 0) AS total
		          FROM refunds r JOIN payments x ON x.id = r.payment_id
		         WHERE x.provider_payment_id = $1) given
		 WHERE p.provider_payment_id = $1`, paymentRef)
	return err
}

// RefundOrder is the order (and its store) a Stripe refund belongs to.
type RefundOrder struct {
	OrderID     string
	BranchID    string
	AmountCents int64
	PaymentRef  string
}

// GetRefundOrder finds which order a Stripe refund id belongs to.
func (s *Store) GetRefundOrder(ctx context.Context, refundID string) (*RefundOrder, error) {
	var r RefundOrder
	err := s.pool.QueryRow(ctx, `
		SELECT r.order_id, COALESCE(p.branch_id::text, ''), r.amount_cents, COALESCE(p.provider_payment_id, '')
		  FROM refunds r LEFT JOIN payments p ON p.id = r.payment_id
		 WHERE r.provider_refund_id = $1`, refundID).Scan(&r.OrderID, &r.BranchID, &r.AmountCents, &r.PaymentRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &r, err
}
