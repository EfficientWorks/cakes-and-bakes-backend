package kds

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"bakehouse/services/orders/internal/store/order"
	"bakehouse/services/orders/ordersapi"
)

// ListKDSOrders returns the orders a KDS board shows, with their lines.
func (s *Repo) ListKDSOrders(ctx context.Context, f ordersapi.KDSOrderFilter) ([]ordersapi.KDSOrderRow, error) {
	if len(f.BranchIDs) == 0 {
		return []ordersapi.KDSOrderRow{}, nil
	}
	where := []string{"branch_id::text = ANY($1)", "status <> ALL($2)"}
	args := []any{f.BranchIDs, kdsClosedStatuses}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}

	switch {
	case f.DateFrom != nil && f.DateTo != nil && f.IncludeOverdue:
		args = append(args, *f.DateFrom, *f.DateTo)
		from, to := len(args)-1, len(args)
		where = append(where, fmt.Sprintf(`(scheduled_date BETWEEN $%d AND $%d
			OR (scheduled_date < $%d AND kot_status IN ('queue','preparing','ready','packing','ready_for_delivery')
			    AND status NOT IN ('cancelled','fulfilled'))
			OR %s)`, from, to, from, fmt.Sprintf(releasedForKDSSQL, fmt.Sprintf("$%d", from), fmt.Sprintf("$%d", to))))
	case f.DateFrom != nil && f.DateTo != nil:
		args = append(args, *f.DateFrom, *f.DateTo)
		from, to := fmt.Sprintf("$%d", len(args)-1), fmt.Sprintf("$%d", len(args))
		// Plus later-dated orders the till started early.
		where = append(where, "(scheduled_date BETWEEN "+from+" AND "+to+" OR "+fmt.Sprintf(releasedForKDSSQL, from, to)+")")
	case f.DateFrom != nil:
		add("scheduled_date >= $%d", *f.DateFrom)
	case f.DateTo != nil:
		add("scheduled_date <= $%d", *f.DateTo)
	}
	if len(f.FulfilmentTypes) > 0 {
		add("lower(fulfilment_type) = ANY($%d)", f.FulfilmentTypes)
	}
	if len(f.Statuses) > 0 {
		add("lower(status) = ANY($%d)", f.Statuses)
	}
	if len(f.KOTStatuses) > 0 {
		add("kot_status = ANY($%d)", f.KOTStatuses)
	}
	if term := strings.TrimSpace(f.Search); term != "" {
		args = append(args, "%"+term+"%")
		i := len(args)
		where = append(where, fmt.Sprintf(
			"(reference ILIKE $%d OR recipient_first_name ILIKE $%d OR recipient_last_name ILIKE $%d OR purchaser_first_name ILIKE $%d OR purchaser_last_name ILIKE $%d OR guest_customer_name ILIKE $%d)",
			i, i, i, i, i, i))
	}

	q := order.SelectSQL + " WHERE " + strings.Join(where, " AND ") +
		" ORDER BY scheduled_date, slot_start, created_at LIMIT 500"
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	var orders []ordersapi.Order
	for rows.Next() {
		var o ordersapi.Order
		if err := rows.Scan(order.ScanTargets(&o)...); err != nil {
			rows.Close()
			return nil, err
		}
		orders = append(orders, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(orders) == 0 {
		return []ordersapi.KDSOrderRow{}, nil
	}

	ids := make([]string, 0, len(orders))
	for _, o := range orders {
		ids = append(ids, o.ID)
	}
	items, err := s.listOrderItemsFor(ctx, ids)
	if err != nil {
		return nil, err
	}
	branches, err := s.branchNames(ctx, f.BranchIDs)
	if err != nil {
		return nil, err
	}
	released, err := s.releasedOrderIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	recalled, err := s.recalledOrderIDs(ctx, ids)
	if err != nil {
		return nil, err
	}

	out := make([]ordersapi.KDSOrderRow, 0, len(orders))
	for _, o := range orders {
		row := ordersapi.KDSOrderRow{Order: o}
		if at, ok := released[o.ID]; ok {
			row.ReleasedAt = &at
		}
		if at, ok := recalled[o.ID]; ok {
			row.RecalledAt = &at
		}
		row.Items = items[o.ID]
		if b, ok := branches[o.BranchID]; ok {
			row.BranchName, row.BranchTimezone = b[0], b[1]
		}
		row.MinPrepMinutes, row.MaxPrepMinutes = prepWindow(row.Items)
		out = append(out, row)
	}
	return out, nil
}

// GetKDSOrder returns one order as a KDS row.
func (s *Repo) GetKDSOrder(ctx context.Context, orderID string) (*ordersapi.KDSOrderRow, error) {
	o, err := s.orders.GetOrderByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	row := &ordersapi.KDSOrderRow{Order: *o}
	if b, err := s.branchNames(ctx, []string{o.BranchID}); err == nil {
		if v, ok := b[o.BranchID]; ok {
			row.BranchName, row.BranchTimezone = v[0], v[1]
		}
	}
	row.MinPrepMinutes, row.MaxPrepMinutes = prepWindow(row.Items)
	if recalled, err := s.recalledOrderIDs(ctx, []string{o.ID}); err == nil {
		if at, ok := recalled[o.ID]; ok {
			row.RecalledAt = &at
		}
	}
	return row, nil
}

func (s *Repo) branchNames(ctx context.Context, ids []string) (map[string][2]string, error) {
	out := map[string][2]string{}
	rows, err := s.pool.Query(ctx, `SELECT id::text, name, timezone FROM branches WHERE id::text = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, name, tz string
		if err := rows.Scan(&id, &name, &tz); err != nil {
			return nil, err
		}
		out[id] = [2]string{name, tz}
	}
	return out, rows.Err()
}

// listOrderItemsFor loads the lines of several orders in one query.
func (s *Repo) listOrderItemsFor(ctx context.Context, orderIDs []string) (map[string][]ordersapi.OrderItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT oi.id, oi.order_id, COALESCE(oi.product_id::text, ''), oi.product_name, oi.variant_id, oi.variant_name, oi.quantity,
		       oi.unit_price_cents, oi.line_total_cents,
		       oi.selected_flavours, oi.selected_addons, oi.selected_presentation, oi.selected_wrap_colour, oi.production_status,
		       oi.batch_no, oi.returned_quantity, oi.send_kot_time, oi.discount_cents, oi.discount_pct, oi.comments,
		       oi.cancelled_at, oi.production_started_at, oi.production_done_at,
		       oi.kot_status, oi.kot_started_at, oi.kot_ready_at,
		       COALESCE(p.min_prep_time, 0), COALESCE(p.min_prep_time_unit, 'minutes'),
		       COALESCE(p.max_prep_time, 0), COALESCE(p.max_prep_time_unit, 'minutes'),
		       COALESCE(pc.id::text, ''), COALESCE(pc.name, ''),
		       oi.gift_message, oi.gift_reason
		  FROM order_items oi
		  LEFT JOIN products p ON p.id = oi.product_id
		  LEFT JOIN product_categories pc ON pc.id = p.category_id
		 WHERE oi.order_id::text = ANY($1)
		 ORDER BY oi.order_id, oi.batch_no, oi.id`, orderIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]ordersapi.OrderItem{}
	for rows.Next() {
		var i ordersapi.OrderItem
		var flavoursRaw, addonsRaw, presRaw, wrapRaw []byte
		if err := rows.Scan(&i.ID, &i.OrderID, &i.ProductID, &i.ProductName, &i.VariantID, &i.VariantName, &i.Quantity,
			&i.UnitPriceCents, &i.LineTotalCents, &flavoursRaw, &addonsRaw, &presRaw, &wrapRaw, &i.ProductionStatus,
			&i.BatchNo, &i.ReturnedQuantity, &i.SendKotTime, &i.ItemDiscountCents, &i.ItemDiscountPct, &i.Comments,
			&i.CancelledAt, &i.ProductionStartedAt, &i.ProductionDoneAt,
			&i.KOTStatus, &i.KOTStartedAt, &i.KOTReadyAt,
			&i.MinPrepTime, &i.MinPrepTimeUnit, &i.MaxPrepTime, &i.MaxPrepTimeUnit,
			&i.CategoryID, &i.CategoryName,
			&i.GiftMessage, &i.GiftReason); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(flavoursRaw, &i.SelectedFlavours)
		_ = json.Unmarshal(addonsRaw, &i.SelectedAddons)
		if presRaw != nil {
			_ = json.Unmarshal(presRaw, &i.SelectedPresentation)
		}
		if wrapRaw != nil {
			_ = json.Unmarshal(wrapRaw, &i.SelectedWrapColour)
		}
		i.MinPrepMinutes = order.PrepMinutes(i.MinPrepTime, i.MinPrepTimeUnit)
		i.MaxPrepMinutes = order.PrepMinutes(i.MaxPrepTime, i.MaxPrepTimeUnit)
		out[i.OrderID] = append(out[i.OrderID], i)
	}
	return out, rows.Err()
}

// ListKOTEvents is the order's kitchen timeline, oldest first.
func (s *Repo) ListKOTEvents(ctx context.Context, orderID string) ([]ordersapi.KOTEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, order_id, order_item_id::text, from_status, to_status, actor_id::text, actor_type, source, note, created_at
		  FROM order_kot_events WHERE order_id = $1 ORDER BY created_at, id`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ordersapi.KOTEvent{}
	for rows.Next() {
		var e ordersapi.KOTEvent
		if err := rows.Scan(&e.ID, &e.OrderID, &e.OrderItemID, &e.FromStatus, &e.ToStatus, &e.ActorID,
			&e.ActorType, &e.Source, &e.Note, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
