package pos

import (
	"context"

	"bakehouse/services/orders/ordersapi"
)

// NextBatchNo returns the kitchen batch a new line should join.
//
// A table orders, the ticket prints, then they order more against the same
// order — that second lot is batch 2. The kitchen has to be able to tell what
// is new from what it has already made, and the number is what a printed
// ticket says.
func (s *Repo) NextBatchNo(ctx context.Context, orderID string) (int, error) {
	var next int
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(batch_no), 0) + 1 FROM order_items WHERE order_id = $1`,
		orderID).Scan(&next)
	return next, err
}

// ApplyOrderTotals writes the arithmetic back onto the order.
func (s *Repo) ApplyOrderTotals(ctx context.Context, orderID string, t ordersapi.OrderTotals) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE orders SET subtotal_cents = $2, discount_cents = $3, discount_pct = $4,
			tax_cents = $5, delivery_fee_cents = $6, service_charge_cents = $7,
			tip_cents = $8, total_cents = $9, tax_pct = COALESCE($10, tax_pct), updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL`,
		orderID, t.SubtotalCents, t.DiscountCents, t.DiscountPct, t.TaxCents,
		t.DeliveryFeeCents, t.ServiceChargeCents, t.TipCents, t.TotalCents, t.TaxPct)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ordersapi.ErrNotFound
	}
	return nil
}

// LinesForTotals reads back what the order now holds, so the totals are
// computed from what was stored rather than from what was asked for.
//
// The difference matters when lines are added to an existing order: the new
// total is the whole order, not the batch that just arrived.
func (s *Repo) LinesForTotals(ctx context.Context, orderID string) ([]ordersapi.OrderLineAmount, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, quantity, returned_quantity, unit_price_cents, line_total_cents, COALESCE(discount_cents, 0), discount_pct
		  FROM order_items
		 WHERE order_id = $1
		   -- A cancelled or returned line stays on the order to explain the docket the
		   -- kitchen was handed, but it is not charged for.
		   AND production_status NOT IN ('cancelled', 'cancel', 'returned', 'return')
		 ORDER BY batch_no, id`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ordersapi.OrderLineAmount{}
	for rows.Next() {
		var l ordersapi.OrderLineAmount
		if err := rows.Scan(&l.ID, &l.Quantity, &l.ReturnedQuantity,
			&l.UnitPriceCents, &l.LineTotalCents, &l.DiscountCents, &l.DiscountPct); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
