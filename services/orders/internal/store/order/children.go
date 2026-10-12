package order

import (
	"context"
	"encoding/json"
	"strings"

	"bakehouse/services/orders/ordersapi"
)

func (s *Repo) attachOrderChildren(ctx context.Context, o *ordersapi.Order) (*ordersapi.Order, error) {
	items, err := s.listOrderItems(ctx, o.ID)
	if err != nil {
		return nil, err
	}
	o.Items = items

	timeline, err := s.listOrderStatusEvents(ctx, o.ID)
	if err != nil {
		return nil, err
	}
	o.Timeline = timeline

	// The full split — with the lines on each bill for an item split — and
	// every tender taken, so the single order read carries the whole
	// settlement picture rather than just the bill ids.
	bills, err := s.bills.ListBills(ctx, o.ID)
	if err != nil {
		return nil, err
	}
	payments, err := s.bills.ListBillPayments(ctx, o.ID)
	if err != nil {
		return nil, err
	}
	o.Bills = bills
	o.Payments = payments

	returns, err := s.returns.ListOrderReturns(ctx, o.ID)
	if err != nil {
		return nil, err
	}
	o.Returns = returns

	o.TotalBillsCount = len(bills)
	o.BillIDs = make([]string, 0, len(bills))
	o.PendingBillIDs = make([]ordersapi.PendingBillRef, 0)
	for _, b := range bills {
		o.BillIDs = append(o.BillIDs, b.ID)
		if strings.ToLower(o.PaymentStatus) != "captured" && strings.ToLower(b.Status) != "paid" && strings.ToLower(b.Status) != "captured" && b.PaidCents < b.AmountCents {
			o.PendingBillIDs = append(o.PendingBillIDs, ordersapi.PendingBillRef{
				ID:          b.ID,
				AmountCents: b.AmountCents,
			})
		}
	}

	if o.CustomerID != nil && strings.TrimSpace(*o.CustomerID) != "" {
		var firstName, lastName, email string
		var mobile *string
		if err := s.pool.QueryRow(ctx, `
			SELECT COALESCE(first_name, ''), COALESCE(last_name, ''), COALESCE(email, ''), mobile_number
			FROM customers WHERE id::text = $1`, strings.TrimSpace(*o.CustomerID)).
			Scan(&firstName, &lastName, &email, &mobile); err == nil {
			o.PurchaserFirstName = firstName
			o.PurchaserLastName = lastName
			o.PurchaserEmail = email
			o.PurchaserPhone = mobile
			// The recipient is read as it was saved: a field the till left
			// out stays empty rather than showing the customer's.
		}
	}

	return o, nil
}

func (s *Repo) listOrderItems(ctx context.Context, orderID string) ([]ordersapi.OrderItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT oi.id, oi.order_id, COALESCE(oi.product_id::text, ''), oi.product_name, oi.variant_id, oi.variant_name, oi.quantity, oi.unit_price_cents, oi.line_total_cents,
		       oi.selected_flavours, oi.selected_addons, oi.selected_presentation, oi.selected_wrap_colour, oi.production_status,
		       oi.batch_no, oi.returned_quantity, oi.send_kot_time, oi.discount_cents, oi.discount_pct, oi.comments, oi.cancelled_at, oi.production_started_at, oi.production_done_at,
		       -- KDS
		       oi.kot_status, oi.kot_started_at, oi.kot_ready_at,
		       COALESCE(p.min_prep_time, 0), COALESCE(p.min_prep_time_unit, 'minutes'),
		       COALESCE(p.max_prep_time, 0), COALESCE(p.max_prep_time_unit, 'minutes'),
		       COALESCE(pc.id::text, ''), COALESCE(pc.name, ''),
		       oi.gift_message, oi.gift_reason
		FROM order_items oi
		LEFT JOIN products p ON p.id = oi.product_id
		LEFT JOIN product_categories pc ON pc.id = p.category_id
		WHERE oi.order_id = $1 ORDER BY oi.batch_no, oi.id`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ordersapi.OrderItem
	for rows.Next() {
		var i ordersapi.OrderItem
		var flavoursRaw, addonsRaw, presRaw, wrapRaw []byte
		if err := rows.Scan(&i.ID, &i.OrderID, &i.ProductID, &i.ProductName, &i.VariantID, &i.VariantName,
			&i.Quantity, &i.UnitPriceCents, &i.LineTotalCents, &flavoursRaw, &addonsRaw, &presRaw, &wrapRaw,
			&i.ProductionStatus, &i.BatchNo, &i.ReturnedQuantity, &i.SendKotTime, &i.ItemDiscountCents, &i.ItemDiscountPct, &i.Comments, &i.CancelledAt, &i.ProductionStartedAt, &i.ProductionDoneAt,
			&i.KOTStatus, &i.KOTStartedAt, &i.KOTReadyAt,
			&i.MinPrepTime, &i.MinPrepTimeUnit, &i.MaxPrepTime, &i.MaxPrepTimeUnit,
			&i.CategoryID, &i.CategoryName,
			&i.GiftMessage, &i.GiftReason); err != nil {
			return nil, err
		}
		i.MinPrepMinutes = PrepMinutes(i.MinPrepTime, i.MinPrepTimeUnit)
		i.MaxPrepMinutes = PrepMinutes(i.MaxPrepTime, i.MaxPrepTimeUnit)
		_ = json.Unmarshal(flavoursRaw, &i.SelectedFlavours)
		_ = json.Unmarshal(addonsRaw, &i.SelectedAddons)
		if presRaw != nil {
			_ = json.Unmarshal(presRaw, &i.SelectedPresentation)
		}
		if wrapRaw != nil {
			_ = json.Unmarshal(wrapRaw, &i.SelectedWrapColour)
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *Repo) listOrderStatusEvents(ctx context.Context, orderID string) ([]ordersapi.OrderStatusEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, order_id, status, actor_id, actor_type, note, created_at
		FROM order_status_events WHERE order_id = $1 ORDER BY created_at`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ordersapi.OrderStatusEvent
	for rows.Next() {
		var e ordersapi.OrderStatusEvent
		if err := rows.Scan(&e.ID, &e.OrderID, &e.Status, &e.ActorID, &e.ActorType, &e.Note, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Repo) listOrderBillIDs(ctx context.Context, orderID string) ([]string, error) {
	bills, err := s.listOrderBills(ctx, orderID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(bills))
	for _, b := range bills {
		out = append(out, b.ID)
	}
	return out, nil
}

func (s *Repo) listOrderBills(ctx context.Context, orderID string) ([]ordersapi.BillSummary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, amount_cents, paid_cents, status FROM order_bills WHERE order_id = $1 ORDER BY sequence_no`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ordersapi.BillSummary{}
	for rows.Next() {
		var b ordersapi.BillSummary
		if err := rows.Scan(&b.ID, &b.AmountCents, &b.PaidCents, &b.Status); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
