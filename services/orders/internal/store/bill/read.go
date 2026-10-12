package bill

import (
	"context"
	"time"

	"bakehouse/services/orders/ordersapi"
)

// ListBills reads an order's split, with what each one has taken.
func (s *Repo) ListBills(ctx context.Context, orderID string) ([]ordersapi.Bill, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, order_id, split_mode, label, sequence_no, amount_cents, paid_cents, status
		  FROM order_bills WHERE order_id = $1 ORDER BY sequence_no`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	bills := []ordersapi.Bill{}
	byID := map[string]int{}
	for rows.Next() {
		var b ordersapi.Bill
		if err := rows.Scan(&b.ID, &b.OrderID, &b.SplitMode, &b.Label,
			&b.SequenceNo, &b.AmountCents, &b.PaidCents, &b.Status); err != nil {
			return nil, err
		}
		byID[b.ID] = len(bills)
		bills = append(bills, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(bills) == 0 {
		return bills, nil
	}

	itemRows, err := s.pool.Query(ctx, `
		SELECT bi.bill_id, bi.order_item_id, oi.product_name, bi.quantity,
		       (oi.unit_price_cents * bi.quantity)
		  FROM order_bill_items bi
		  JOIN order_items oi ON oi.id = bi.order_item_id
		 WHERE bi.bill_id IN (SELECT id FROM order_bills WHERE order_id = $1)`, orderID)
	if err != nil {
		return nil, err
	}
	defer itemRows.Close()
	for itemRows.Next() {
		var billID string
		var it ordersapi.BillItem
		if err := itemRows.Scan(&billID, &it.OrderItemID, &it.ProductName, &it.Quantity, &it.AmountCents); err != nil {
			return nil, err
		}
		if i, ok := byID[billID]; ok {
			bills[i].Items = append(bills[i].Items, it)
		}
	}
	return bills, itemRows.Err()
}

// ListBillPayments reads what has been taken against an order.
func (s *Repo) ListBillPayments(ctx context.Context, orderID string) ([]ordersapi.BillPayment, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, bill_id, method, amount_cents, tip_cents, tendered_cents,
		       COALESCE(reference, ''), taken_by, created_at, provider, provider_ref, payment_link_id
		  FROM bill_payments WHERE order_id = $1 ORDER BY created_at`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ordersapi.BillPayment{}
	for rows.Next() {
		var p ordersapi.BillPayment
		var ts time.Time
		if err := rows.Scan(&p.ID, &p.BillID, &p.Method, &p.AmountCents, &p.TipCents,
			&p.TenderedCents, &p.Reference, &p.TakenBy, &ts, &p.Provider, &p.ProviderRef, &p.PaymentLinkID); err != nil {
			return nil, err
		}
		p.CreatedAt = ts.Format(time.RFC3339)
		if p.TenderedCents != nil && *p.TenderedCents > p.AmountCents {
			p.ChangeCents = *p.TenderedCents - p.AmountCents
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// HasOpenBills reports whether the order has a split with at least one share
// not yet fully paid — i.e. a split payment still in progress.
func (s *Repo) HasOpenBills(ctx context.Context, orderID string) (bool, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM order_bills
		 WHERE order_id = $1
		   AND lower(COALESCE(status, 'open')) NOT IN ('paid', 'captured', 'settled')
		   AND paid_cents < amount_cents`, orderID).Scan(&n)
	return n > 0, err
}
