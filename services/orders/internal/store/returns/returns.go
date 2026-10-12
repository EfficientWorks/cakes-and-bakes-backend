package returns

import (
	"context"
	"fmt"

	"bakehouse/pkg/timex"
	"bakehouse/services/orders/internal/store/shared"
	"bakehouse/services/orders/ordersapi"
)

// RecordReturn writes a return and takes the quantity off the lines.
//
// One transaction: a return that reduced the lines but lost its own record
// would leave the books unexplainable, and one that recorded itself without
// reducing them would have the order still claiming to owe for goods that came
// back over the counter.
//
// beforeCommit, when given, runs with the priced return just before it is
// committed; an error rolls the return back. It is how a card refund is made
// first and the return recorded only once the provider has accepted it.
func (s *Repo) RecordReturn(ctx context.Context, orderID string, r ordersapi.ReturnRequest, staffID *string, price ordersapi.ReturnPricer, beforeCommit func(ordersapi.ReturnCharges) error) (string, ordersapi.ReturnCharges, error) {
	var none ordersapi.ReturnCharges
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", none, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// A whole-order return is every outstanding line at once, resolved here so
	// the caller does not have to enumerate them. Cancelled lines were never
	// charged for, so they are not handed back either.
	lines := r.Items
	if r.FullReturn {
		rows, err := tx.Query(ctx, `
			SELECT id, quantity - returned_quantity
			  FROM order_items WHERE order_id = $1 AND quantity > returned_quantity
			   AND production_status NOT IN ('cancelled', 'cancel', 'returned', 'return')`, orderID)
		if err != nil {
			return "", none, err
		}
		lines = nil
		for rows.Next() {
			var item ordersapi.ReturnLine
			if err := rows.Scan(&item.OrderItemID, &item.Quantity); err != nil {
				rows.Close()
				return "", none, err
			}
			lines = append(lines, item)
		}
		rows.Close()
	}
	if len(lines) == 0 {
		return "", none, fmt.Errorf("nothing to return")
	}

	var returnID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO order_returns (order_id, reason, is_full_return, returned_by, created_at)
		VALUES ($1,$2,$3,$4, COALESCE($5::timestamptz, NOW())) RETURNING id`,
		orderID, shared.NilIfEmptyStr(r.Reason), r.FullReturn, staffID, timex.EventTimeFrom(ctx)).Scan(&returnID); err != nil {
		return "", none, err
	}

	/*
	 * Take the quantity off each line, reading back what pricing needs.
	 *
	 * Guarded by the quantity actually outstanding so the same item cannot be
	 * handed back twice: the UPDATE is the guard, and a row that fails it
	 * returns nothing.
	 */
	inputs := make([]ordersapi.ReturnLineInput, 0, len(lines))
	for _, line := range lines {
		in := ordersapi.ReturnLineInput{OrderItemID: line.OrderItemID, Quantity: line.Quantity}
		err := tx.QueryRow(ctx, `
			UPDATE order_items
			   SET returned_quantity = returned_quantity + $2
			 WHERE id = $1 AND order_id = $3
			   AND quantity - returned_quantity >= $2
			RETURNING unit_price_cents, quantity, COALESCE(discount_cents, 0), discount_pct`,
			line.OrderItemID, line.Quantity, orderID).Scan(&in.UnitPriceCents, &in.LineQuantity, &in.LineDiscountCents, &in.LineDiscountPct)
		if err != nil {
			return "", none, fmt.Errorf("line %s cannot return %d: %w", line.OrderItemID, line.Quantity, err)
		}
		inputs = append(inputs, in)
	}

	// Each line priced on its own (its own discount, its share of the order
	// discount, tax on the rest); the return's totals are their sums.
	var priced []ordersapi.ReturnLineCharges
	var charges ordersapi.ReturnCharges
	if price != nil {
		priced, charges = price(inputs)
	} else {
		for _, in := range inputs {
			g := in.UnitPriceCents * int64(in.Quantity)
			priced = append(priced, ordersapi.ReturnLineCharges{OrderItemID: in.OrderItemID, Quantity: in.Quantity,
				ReturnCharges: ordersapi.ReturnCharges{GrossCents: g, RefundCents: g}})
			charges.GrossCents += g
			charges.RefundCents += g
		}
	}
	for _, p := range priced {
		if _, err := tx.Exec(ctx, `
			INSERT INTO order_return_items (return_id, order_item_id, quantity, amount_cents,
				discount_pct, discount_cents, tax_pct, tax_cents, refund_cents)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			returnID, p.OrderItemID, p.Quantity, p.GrossCents,
			p.DiscountPct, p.DiscountCents, p.TaxPct, p.TaxCents, p.RefundCents); err != nil {
			return "", none, err
		}
	}
	total := charges.GrossCents

	// Return-based: on the return itself.
	if _, err := tx.Exec(ctx, `
		UPDATE order_returns
		   SET amount_cents = $2, discount_pct = $3, discount_cents = $4,
		       tax_pct = $5, tax_cents = $6, refund_cents = $7
		 WHERE id = $1`,
		returnID, total, charges.DiscountPct, charges.DiscountCents,
		charges.TaxPct, charges.TaxCents, charges.RefundCents); err != nil {
		return "", none, err
	}

	// Order-based: running totals on the order; the rates are the latest.
	if _, err := tx.Exec(ctx, `
		UPDATE orders
		   SET return_amount_cents   = return_amount_cents + $2,
		       return_discount_cents = return_discount_cents + $3,
		       return_tax_cents      = return_tax_cents + $4,
		       return_total_cents    = return_total_cents + $5,
		       return_discount_pct   = COALESCE($6, return_discount_pct),
		       return_tax_pct        = COALESCE($7, return_tax_pct)
		 WHERE id = $1`,
		orderID, total, charges.DiscountCents, charges.TaxCents, charges.RefundCents,
		charges.DiscountPct, charges.TaxPct); err != nil {
		return "", none, err
	}

	// Fully handed back means the order is a return, not a sale. Cancelled
	// lines do not count as still outstanding.
	var outstanding int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(quantity - returned_quantity), 0) FROM order_items
		 WHERE order_id = $1 AND production_status NOT IN ('cancelled', 'cancel', 'returned', 'return')`,
		orderID).Scan(&outstanding); err != nil {
		return "", none, err
	}
	/* if outstanding == 0 {
		if _, err := tx.Exec(ctx,
			`UPDATE orders SET status = $2, updated_at = NOW() WHERE id = $1`, orderID, ordersapi.StatusReturned); err != nil {
			return "", none, err
		}
	} */
	if outstanding == 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE orders
			   SET status = $2, returned_by = COALESCE($3, returned_by), updated_at = NOW(),
			       end_time = COALESCE(end_time, $4::timestamptz, NOW())
			 WHERE id = $1`, orderID, ordersapi.StatusReturned, staffID, timex.EventTimeFrom(ctx)); err != nil {
			return "", none, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO order_status_events (order_id, status, actor_id, actor_type, note, created_at)
			VALUES ($1, $2, $3, 'staff', $4, COALESCE($5::timestamptz, NOW()))`,
			orderID, ordersapi.StatusReturned, staffID, shared.NilIfEmptyStr(r.Reason), timex.EventTimeFrom(ctx)); err != nil {
			return "", none, err
		}
	} else {
		if _, err := tx.Exec(ctx, `
			UPDATE orders SET returned_by = COALESCE($2, returned_by), updated_at = NOW() WHERE id = $1`,
			orderID, staffID); err != nil {
			return "", none, err
		}
	}

	if beforeCommit != nil {
		if err := beforeCommit(charges); err != nil {
			return "", none, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return "", none, err
	}
	return returnID, charges, nil
}

// ListOrderReturns reads every return on an order, oldest first, with its
// lines and its own discount and tax.
func (s *Repo) ListOrderReturns(ctx context.Context, orderID string) ([]ordersapi.OrderReturn, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.reason, r.is_full_return, r.returned_by::text,
		       (SELECT COALESCE(NULLIF(TRIM(CONCAT(su.first_name, ' ', su.last_name)), ''), su.email)
		          FROM staff_users su WHERE su.id = r.returned_by),
		       r.created_at, r.amount_cents, r.discount_pct, r.discount_cents,
		       r.tax_pct, r.tax_cents, r.refund_cents
		  FROM order_returns r
		 WHERE r.order_id = $1
		 ORDER BY r.created_at, r.id`, orderID)
	if err != nil {
		return nil, err
	}
	out := []ordersapi.OrderReturn{}
	index := map[string]int{}
	for rows.Next() {
		var r ordersapi.OrderReturn
		if err := rows.Scan(&r.ID, &r.Reason, &r.IsFullReturn, &r.ReturnedBy, &r.ReturnedByName,
			&r.CreatedAt, &r.GrossCents, &r.DiscountPct, &r.DiscountCents,
			&r.TaxPct, &r.TaxCents, &r.RefundCents); err != nil {
			rows.Close()
			return nil, err
		}
		r.Items = []ordersapi.OrderReturnItem{}
		index[r.ID] = len(out)
		out = append(out, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}

	itemRows, err := s.pool.Query(ctx, `
		SELECT ri.return_id, ri.order_item_id, COALESCE(oi.product_name, ''), ri.quantity, ri.amount_cents,
		       ri.discount_pct, ri.discount_cents, ri.tax_pct, ri.tax_cents, COALESCE(ri.refund_cents, ri.amount_cents)
		  FROM order_return_items ri
		  JOIN order_returns r ON r.id = ri.return_id
		  LEFT JOIN order_items oi ON oi.id = ri.order_item_id
		 WHERE r.order_id = $1
		 ORDER BY ri.return_id, ri.id`, orderID)
	if err != nil {
		return nil, err
	}
	defer itemRows.Close()
	for itemRows.Next() {
		var returnID string
		var it ordersapi.OrderReturnItem
		if err := itemRows.Scan(&returnID, &it.OrderItemID, &it.ProductName, &it.Quantity, &it.AmountCents,
			&it.DiscountPct, &it.DiscountCents, &it.TaxPct, &it.TaxCents, &it.RefundCents); err != nil {
			return nil, err
		}
		if i, ok := index[returnID]; ok {
			out[i].Items = append(out[i].Items, it)
		}
	}
	return out, itemRows.Err()
}
