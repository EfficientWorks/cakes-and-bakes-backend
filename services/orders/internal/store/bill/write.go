package bill

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"bakehouse/pkg/timex"
	"bakehouse/services/orders/internal/store/shared"
	"bakehouse/services/orders/ordersapi"
)

// ReplaceBills writes a fresh split, discarding any previous one.
//
// Splits are re-cut freely at a counter — somebody joins the table, somebody
// leaves — so this replaces rather than accumulates. Bills that have already
// taken money are refused rather than silently dropped: money that has been
// handed over is not something a UI gesture should be able to erase.
func (s *Repo) ReplaceBills(ctx context.Context, orderID string, mode string, bills []ordersapi.Bill) ([]ordersapi.Bill, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var paidCount int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM order_bills WHERE order_id = $1 AND paid_cents > 0`,
		orderID).Scan(&paidCount); err != nil {
		return nil, err
	}
	if paidCount > 0 {
		return nil, fmt.Errorf("this order already has settled bills")
	}

	if _, err := tx.Exec(ctx, `DELETE FROM order_bills WHERE order_id = $1`, orderID); err != nil {
		return nil, err
	}

	// The mode rides on the order too, so the order GET can say how the bill
	// was split without reading the bills.
	if _, err := tx.Exec(ctx,
		`UPDATE orders SET split_mode = $2, updated_at = NOW() WHERE id = $1`, orderID, mode); err != nil {
		return nil, err
	}

	out := make([]ordersapi.Bill, 0, len(bills))
	for i, b := range bills {
		label := b.Label
		if label == "" {
			label = fmt.Sprintf("Bill %d of %d", i+1, len(bills))
		}
		var id string
		if err := tx.QueryRow(ctx, `
			INSERT INTO order_bills (order_id, split_mode, label, sequence_no, amount_cents)
			VALUES ($1,$2,$3,$4,$5) RETURNING id`,
			orderID, mode, label, i+1, b.AmountCents).Scan(&id); err != nil {
			return nil, err
		}

		// Only an item split carries lines; the other two are amounts.
		for _, it := range b.Items {
			if _, err := tx.Exec(ctx, `
				INSERT INTO order_bill_items (bill_id, order_item_id, quantity)
				VALUES ($1,$2,$3)`, id, it.OrderItemID, it.Quantity); err != nil {
				return nil, err
			}
		}

		b.ID = id
		b.OrderID = orderID
		b.SplitMode = mode
		b.Label = label
		b.SequenceNo = i + 1
		b.Status = "open"
		out = append(out, b)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

/*
 * Taking money.
 *
 * The payment is written, the bill's running total goes up, and if that
 * settles it the bill closes — all in one transaction, because a payment that
 * was recorded without closing its bill would be taken twice by the next
 * person to look at the screen.
 *
 * Returns whether the whole order is now settled, so the caller can close it
 * without having to ask a second question and race with another till.
 */
func (s *Repo) RecordBillPayment(ctx context.Context, orderID string, p ordersapi.BillPayment, serviceChargeCents *int64) (settledBill bool, settledOrder bool, change int64, err error) {
	var billID *string
	if p.BillID != nil && strings.TrimSpace(*p.BillID) != "" {
		trimmed := strings.TrimSpace(*p.BillID)
		billID = &trimmed
	}
	p.BillID = billID

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, false, 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// A service charge supplied at payment time is stored on the order. On an
	// unsplit order it also moves the total by the difference, so the order
	// is worth what was charged (the till's amount includes it). A split
	// order already has it in its total and its shares: it was set when the
	// order was split.
	if serviceChargeCents != nil {
		if _, err := tx.Exec(ctx, `
			UPDATE orders
			   SET total_cents = total_cents + CASE
			           WHEN NOT EXISTS (SELECT 1 FROM order_bills ob WHERE ob.order_id = orders.id)
			           THEN $2 - service_charge_cents ELSE 0 END,
			       service_charge_cents = $2,
			       updated_at = NOW()
			 WHERE id = $1`, orderID, *serviceChargeCents); err != nil {
			return false, false, 0, err
		}
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO bill_payments (order_id, bill_id, method, amount_cents, tip_cents,
			tendered_cents, reference, taken_by, created_at, provider, provider_ref, payment_link_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8, COALESCE($9::timestamptz, NOW()), $10, $11, $12)`,
		orderID, p.BillID, p.Method, p.AmountCents, p.TipCents,
		p.TenderedCents, shared.NilIfEmptyStr(p.Reference), p.TakenBy, timex.EventTimeFrom(ctx),
		p.Provider, p.ProviderRef, p.PaymentLinkID); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "bill_payments_provider_ref_idx" {
			return false, false, 0, ErrDuplicatePayment
		}
		return false, false, 0, err
	}

	/*
	 * A tip taken with the payment lands on the order.
	 *
	 * It used to stop at bill_payments.tip_cents, so the order GET reported
	 * tip = 0 for an order whose payment plainly carried one. It is added to
	 * both tip_cents and total_cents: the order total includes the tip
	 * everywhere else (recalculatePOSTotals), and keeping the two in step is
	 * what lets the settlement check below stay honest.
	 */
	if p.TipCents > 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE orders
			   SET tip_cents   = tip_cents + $2,
			       total_cents = total_cents + $2,
			       updated_at  = NOW()
			 WHERE id = $1`, orderID, p.TipCents); err != nil {
			return false, false, 0, err
		}
	}

	// Whoever takes money has touched the order.
	if p.TakenBy != nil && strings.TrimSpace(*p.TakenBy) != "" {
		if _, err := tx.Exec(ctx,
			`UPDATE orders SET updated_biller_id = $2 WHERE id = $1`, orderID, strings.TrimSpace(*p.TakenBy)); err != nil {
			return false, false, 0, err
		}
	}

	// Cash handed over above what was owed comes back as change. Recorded as
	// the difference rather than a second row, because change is not a payment.
	if p.TenderedCents != nil && *p.TenderedCents > p.AmountCents {
		change = *p.TenderedCents - p.AmountCents
	}

	if p.BillID != nil {
		if err := tx.QueryRow(ctx, `
			UPDATE order_bills
			   SET paid_cents = paid_cents + $2,
			       status = CASE WHEN paid_cents + $2 >= amount_cents THEN 'paid' ELSE status END,
			       updated_at = NOW()
			 WHERE id = $1
			RETURNING status = 'paid'`, *p.BillID, p.AmountCents).Scan(&settledBill); err != nil {
			return false, false, 0, err
		}
	}

	/*
	 * The order is settled when nothing is outstanding.
	 *
	 * Measured against the order's own total rather than the sum of the bills:
	 * a split that does not quite add up — a rounding penny, a bill voided
	 * after the fact — should not leave an order that can never close.
	 */
	var owed, taken int64
	if err := tx.QueryRow(ctx,
		`SELECT total_cents FROM orders WHERE id = $1`, orderID).Scan(&owed); err != nil {
		return false, false, 0, err
	}
	// Tips count towards what was taken because they were folded into
	// total_cents above; without them a tipped order could never settle.
	var methods []string
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount_cents + tip_cents), 0),
		       COALESCE(ARRAY_AGG(DISTINCT method), '{}')
		  FROM bill_payments WHERE order_id = $1`,
		orderID).Scan(&taken, &methods); err != nil {
		return false, false, 0, err
	}

	if taken >= owed {
		settledOrder = true
		/*
		 * The payment method is written now, not at insert: the order did
		 * not know how it would be paid until this moment. One tender across
		 * every payment names that tender; more than one is 'mixed'.
		 */
		method := "mixed"
		if len(methods) == 1 {
			method = methods[0]
		}
		if _, err := tx.Exec(ctx, `
			UPDATE orders SET payment_status = 'captured', payment_method = $3, updated_at = NOW(),
				settled_by = COALESCE(settled_by, $2),
				cash_collected_at = CASE WHEN $3 = 'cash' THEN COALESCE(cash_collected_at, $4::timestamptz, NOW()) ELSE cash_collected_at END,
				total_duration_secs = CASE WHEN total_duration_secs IS NULL THEN EXTRACT(EPOCH FROM COALESCE($4::timestamptz, NOW()) - created_at)::INTEGER ELSE total_duration_secs END,
				end_time = COALESCE(end_time, $4::timestamptz, NOW())
			 WHERE id = $1`, orderID, p.TakenBy, method, timex.EventTimeFrom(ctx)); err != nil {
			return false, false, 0, err
		}
		// Any bill still open on a fully paid order is closed with it: the
		// money is in, and leaving a bill open would invite taking it twice.
		if _, err := tx.Exec(ctx, `
			UPDATE order_bills SET status = 'paid', updated_at = NOW()
			 WHERE order_id = $1 AND status = 'open'`, orderID); err != nil {
			return false, false, 0, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return false, false, 0, err
	}
	return settledBill, settledOrder, change, nil
}

// ClearBills removes an order's split entirely and resets split_mode.
// Refused once any share has taken money — the same rule as ReplaceBills.
// Returns (removed count, error).
func (s *Repo) ClearBills(ctx context.Context, orderID string) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var paidCount int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM order_bills WHERE order_id = $1 AND paid_cents > 0`,
		orderID).Scan(&paidCount); err != nil {
		return 0, err
	}
	if paidCount > 0 {
		return 0, ErrBillsSettled
	}

	// Line allocations first: order_bill_items references order_bills.
	if _, err := tx.Exec(ctx,
		`DELETE FROM order_bill_items WHERE bill_id IN (SELECT id FROM order_bills WHERE order_id = $1)`,
		orderID); err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM order_bills WHERE order_id = $1`, orderID)
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE orders SET split_mode = NULL, updated_at = NOW() WHERE id = $1`, orderID); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// AddOrderCharges adds a tip and service charge to an order and its total,
// and to the split bill when one is named, in one transaction. Used when a
// payment link carries them (applyLinkCharges).
func (s *Repo) AddOrderCharges(ctx context.Context, orderID string, billID *string, tipCents, serviceChargeCents int64) error {
	add := tipCents + serviceChargeCents
	if add <= 0 {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		UPDATE orders
		   SET tip_cents = tip_cents + $2,
		       service_charge_cents = service_charge_cents + $3,
		       total_cents = total_cents + $2 + $3,
		       updated_at = NOW()
		 WHERE id = $1 AND deleted_at IS NULL`, orderID, tipCents, serviceChargeCents); err != nil {
		return err
	}
	if billID != nil {
		if _, err := tx.Exec(ctx, `
			UPDATE order_bills SET amount_cents = amount_cents + $3, updated_at = NOW()
			 WHERE id = $1 AND order_id = $2`, *billID, orderID, add); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
