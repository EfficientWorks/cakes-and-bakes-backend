package order

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"bakehouse/pkg/tenant"
	"bakehouse/services/orders/ordersapi"
)

func (s *Repo) ListOrders(ctx context.Context, f ordersapi.ListOrdersFilter) ([]ordersapi.Order, int, error) {
	whereClause, args := buildListOrdersWhere(ctx, f)
	var total int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM (SELECT * FROM orders WHERE deleted_at IS NULL) orders WHERE "+whereClause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	page, limit := f.Page, f.Limit
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 25
	}
	args = append(args, limit, (page-1)*limit)
	// item_count via correlated subquery rather than a JOIN + GROUP BY —
	// list views need a count, not the full item rows (ADM-01: "item
	// count" is part of the order list shape).
	// Most recent first. created_at is what a counter means by "recent";
	// sort=updated puts the last-touched order on top instead. The id
	// tie-break keeps paging stable when two orders share a timestamp.
	orderBy := "created_at DESC, id DESC"
	if strings.ToLower(strings.TrimSpace(f.Sort)) == "updated" {
		orderBy = "COALESCE(updated_at, created_at) DESC, created_at DESC, id DESC"
	}
	q := orderListSelectWithCount + " WHERE " + whereClause + fmt.Sprintf(" ORDER BY %s LIMIT $%d OFFSET $%d", orderBy, len(args)-1, len(args))
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []ordersapi.Order
	for rows.Next() {
		o, err := scanOrderWithCount(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *o)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	if len(out) > 0 {
		orderIDs := make([]string, len(out))
		byID := make(map[string]int, len(out))
		for i, o := range out {
			orderIDs[i] = o.ID
			byID[o.ID] = i
			out[i].BillIDs = []string{}
			out[i].PendingBillIDs = []ordersapi.PendingBillRef{}
			out[i].TotalBillsCount = 0
		}
		billRows, err := s.pool.Query(ctx, `SELECT order_id, id::text, amount_cents, paid_cents, status FROM order_bills WHERE order_id = ANY($1) ORDER BY sequence_no`, orderIDs)
		if err == nil {
			defer billRows.Close()
			for billRows.Next() {
				var ordID, billID, status string
				var amountCents, paidCents int64
				if err := billRows.Scan(&ordID, &billID, &amountCents, &paidCents, &status); err == nil {
					if idx, ok := byID[ordID]; ok {
						out[idx].BillIDs = append(out[idx].BillIDs, billID)
						out[idx].TotalBillsCount++
						if strings.ToLower(out[idx].PaymentStatus) != "captured" && strings.ToLower(status) != "paid" && strings.ToLower(status) != "captured" && paidCents < amountCents {
							out[idx].PendingBillIDs = append(out[idx].PendingBillIDs, ordersapi.PendingBillRef{
								ID:          billID,
								AmountCents: amountCents,
							})
						}
					}
				}
			}
		}

		itemRows, err := s.pool.Query(ctx, `
			SELECT id, order_id, COALESCE(product_id::text, ''), product_name, variant_id, variant_name, quantity, unit_price_cents, line_total_cents,
			       selected_flavours, selected_addons, selected_presentation, selected_wrap_colour, production_status,
			       batch_no, returned_quantity, send_kot_time, discount_cents, discount_pct, comments, cancelled_at, production_started_at, production_done_at,
			       gift_message, gift_reason
			FROM order_items WHERE order_id = ANY($1) ORDER BY batch_no, id`, orderIDs)
		if err == nil {
			defer itemRows.Close()
			for itemRows.Next() {
				var i ordersapi.OrderItem
				var ordID string
				var flavoursRaw, addonsRaw, presRaw, wrapRaw []byte
				if err := itemRows.Scan(&i.ID, &ordID, &i.ProductID, &i.ProductName, &i.VariantID, &i.VariantName,
					&i.Quantity, &i.UnitPriceCents, &i.LineTotalCents, &flavoursRaw, &addonsRaw, &presRaw, &wrapRaw,
					&i.ProductionStatus, &i.BatchNo, &i.ReturnedQuantity, &i.SendKotTime, &i.ItemDiscountCents, &i.ItemDiscountPct, &i.Comments, &i.CancelledAt, &i.ProductionStartedAt, &i.ProductionDoneAt,
					&i.GiftMessage, &i.GiftReason); err == nil {
					i.OrderID = ordID
					_ = json.Unmarshal(flavoursRaw, &i.SelectedFlavours)
					_ = json.Unmarshal(addonsRaw, &i.SelectedAddons)
					if presRaw != nil {
						_ = json.Unmarshal(presRaw, &i.SelectedPresentation)
					}
					if wrapRaw != nil {
						_ = json.Unmarshal(wrapRaw, &i.SelectedWrapColour)
					}
					if idx, ok := byID[ordID]; ok {
						out[idx].Items = append(out[idx].Items, i)
					}
				}
			}
		}
	}

	return out, total, nil
}

// SummarizeOrders totals the filtered set: how many orders, their combined
// total, and how many are still in "scheduled".
func (s *Repo) SummarizeOrders(ctx context.Context, f ordersapi.ListOrdersFilter) (ordersapi.OrderListSummary, error) {
	var out ordersapi.OrderListSummary

	// Count and total: exactly the rows the page is cut from.
	//
	// The revenue is net of cancellations and returns: a cancelled, fully
	// returned or refunded order counts nothing, and a partly returned one
	// counts its total, which the return already recomputed from the lines
	// that were kept.
	whereClause, args := buildListOrdersWhere(ctx, f)
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*),
		       COALESCE(SUM(total_cents) FILTER (WHERE lower(status) NOT IN ('cancelled', 'returned', 'refunded')), 0),
		       count(*) FILTER (WHERE lower(status) = 'cancelled'),
		       COALESCE(SUM(total_cents) FILTER (WHERE lower(status) = 'cancelled'), 0),
		       count(*) FILTER (WHERE lower(status) <> 'cancelled' AND (lower(status) IN ('returned', 'refunded') OR return_total_cents > 0)),
		       COALESCE(SUM(CASE
		           WHEN lower(status) = 'cancelled' THEN 0
		           WHEN lower(status) IN ('returned', 'refunded') THEN return_total_cents + total_cents
		           ELSE return_total_cents END), 0)
		  FROM (SELECT * FROM orders WHERE deleted_at IS NULL) orders
		 WHERE `+whereClause, args...).Scan(&out.TotalOrders, &out.TotalCents,
		&out.CancelledOrders, &out.CancelledCents, &out.ReturnedOrders, &out.ReturnedCents); err != nil {
		return out, err
	}
	// Gross: what the set was rung up for before anything came off.
	out.GrossCents = out.TotalCents + out.CancelledCents + out.ReturnedCents

	// Scheduled: same branch / type / channel / date / search filters, but
	// ignoring status — the tab's own status rule would otherwise hide the
	// very orders being counted (a delivery tab lists open deliveries;
	// its badge is the scheduled ones not yet in that list).
	//
	// On a POS open tab it is the tab's Scheduled sub-tab: open orders due
	// on a later day.
	if isOpenTab(strings.ToLower(strings.TrimSpace(f.OrderFilterType))) {
		sf := f
		sf.Status = ordersapi.StatusScheduled
		whereSched, argsSched := buildListOrdersWhere(ctx, sf)
		if err := s.pool.QueryRow(ctx, `
			SELECT count(*)
			  FROM (SELECT * FROM orders WHERE deleted_at IS NULL) orders
			 WHERE `+whereSched, argsSched...).Scan(&out.ScheduledOrders); err != nil {
			return out, err
		}
		return out, nil
	}
	whereNoStatus, argsNoStatus := buildListOrdersWhereOpt(ctx, f, true)
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*)
		  FROM (SELECT * FROM orders WHERE deleted_at IS NULL) orders
		 WHERE `+whereNoStatus+` AND status = 'scheduled'`, argsNoStatus...).Scan(&out.ScheduledOrders); err != nil {
		return out, err
	}
	return out, nil
}

// buildListOrdersWhere turns a filter into a WHERE clause and its args. Shared
// by ListOrders and SummarizeOrders so the totals always describe the same
// rows the page was cut from.
func buildListOrdersWhere(ctx context.Context, f ordersapi.ListOrdersFilter) (string, []interface{}) {
	return buildListOrdersWhereOpt(ctx, f, false)
}

// buildListOrdersWhereOpt is buildListOrdersWhere with the option to leave
// out every status condition (explicit `status=`, the statuses an
// orderFilterType implies, and the "completed" money test). Used to count
// the scheduled orders behind a tab: a "delivery" tab excludes scheduled
// orders from its list, yet the counter still wants to know how many
// scheduled deliveries are waiting.
func buildListOrdersWhereOpt(ctx context.Context, f ordersapi.ListOrdersFilter, ignoreStatus bool) (string, []interface{}) {
	where := []string{"1=1"}
	args := []interface{}{}
	add := func(cond string, val interface{}) {
		args = append(args, val)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	// Only the caller's business's orders, whatever else is filtered.
	if tenant.Scoped(ctx) {
		add("business_id IS NOT DISTINCT FROM $%d", tenant.BusinessParam(ctx))
	}
	if f.BranchID != "" {
		add("branch_id = $%d", f.BranchID)
	}
	if f.CustomerID != "" {
		add("customer_id = $%d", f.CustomerID)
	}
	// orderFilterType supplies DEFAULTS; an explicit status / fulfilmentType /
	// channel param REPLACES the implied value for that dimension.
	explicitStatuses := splitFilterValues(f.Status)
	explicitFulfilment := splitFilterValues(f.FulfilmentType)
	explicitChannel := splitFilterValues(f.Channel)

	var impliedChannel, impliedFulfilment string
	var impliedStatuses []string
	impliedStatusIn := false
	filterType := strings.ToLower(strings.TrimSpace(f.OrderFilterType))
	if filterType != "" && filterType != "all" {
		if ch, ft, statuses, in, ok := ordersapi.OrderFilterTypeConditions(filterType); ok {
			impliedChannel, impliedFulfilment, impliedStatuses, impliedStatusIn = ch, ft, statuses, in
		}
	}

	/*
	 * "completed" is about money, not the workflow: an order is done when
	 * it has been paid in full (payment captured, or every split share
	 * paid), or it was returned, refunded or cancelled. So it is applied as
	 * its own condition, and an explicit status NARROWS it rather than
	 * replacing it — orderFilterType=completed&status=scheduled is "the
	 * scheduled orders that are already paid".
	 */
	argAt := func(v interface{}) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if filterType == ordersapi.OrderFilterTypeCompleted && !ignoreStatus {
		where = append(where, orderHistorySQL)
		impliedStatuses = nil // handled above; do not also exclude by status
		if len(explicitStatuses) > 0 {
			where = append(where, historyStatusSQL(explicitStatuses, argAt))
			explicitStatuses = nil
		}
	}
	// The open tabs: today's orders, or with status=scheduled the ones due
	// on a later day (see order_tabs.go). Any other explicit status keeps
	// replacing the tab's rule, as before.
	if isOpenTab(filterType) && !ignoreStatus && (len(explicitStatuses) == 0 || asksScheduled(explicitStatuses)) {
		where = append(where, openTabSQL(asksScheduled(explicitStatuses)))
		impliedStatuses, explicitStatuses = nil, nil
	}

	switch {
	case len(explicitChannel) > 0:
		add("lower(channel) = ANY($%d)", explicitChannel)
	case impliedChannel != "":
		add("lower(channel) = $%d", impliedChannel)
	}

	switch {
	case len(explicitFulfilment) > 0:
		add("lower(fulfilment_type) = ANY($%d)", explicitFulfilment)
	case impliedFulfilment != "":
		add("lower(fulfilment_type) = $%d", impliedFulfilment)
	}

	switch {
	case ignoreStatus:
		// no status condition at all
	case len(explicitStatuses) > 0:
		add("lower(status) = ANY($%d)", explicitStatuses)
	case len(impliedStatuses) > 0 && impliedStatusIn:
		add("status = ANY($%d)", impliedStatuses)
	case len(impliedStatuses) > 0:
		add("status <> ALL($%d)", impliedStatuses)
	}

	// KDS: kitchen status filter. Independent of orderFilterType.
	if kot := splitFilterValues(f.KOTStatus); len(kot) > 0 {
		add("lower(kot_status) = ANY($%d)", kot)
	}

	if f.PaymentType != "" && strings.ToLower(strings.TrimSpace(f.PaymentType)) != "all" {
		pt := strings.ToLower(strings.TrimSpace(f.PaymentType))
		args = append(args, pt)
		idx := len(args)
		if pt == "cash" {
			where = append(where, fmt.Sprintf("(lower(payment_method) IN ('cash', 'cash_on_delivery') OR EXISTS (SELECT 1 FROM bill_payments bp WHERE bp.order_id = orders.id AND lower(bp.method) = $%d))", idx))
		} else {
			where = append(where, fmt.Sprintf("(lower(payment_method) = $%d OR EXISTS (SELECT 1 FROM bill_payments bp WHERE bp.order_id = orders.id AND lower(bp.method) = $%d) OR EXISTS (SELECT 1 FROM payments p WHERE p.order_id = orders.id AND lower(p.method) = $%d))", idx, idx, idx))
		}
	}
	if f.DateFrom != nil {
		add("scheduled_date >= $%d", *f.DateFrom)
	}
	if f.DateTo != nil {
		add("scheduled_date <= $%d", *f.DateTo)
	}
	if f.Search != "" {
		args = append(args, "%"+f.Search+"%")
		idx := len(args)
		where = append(where, fmt.Sprintf(
			"(reference ILIKE $%d OR purchaser_first_name ILIKE $%d OR purchaser_last_name ILIKE $%d OR recipient_first_name ILIKE $%d OR recipient_last_name ILIKE $%d OR recipient_phone ILIKE $%d)",
			idx, idx, idx, idx, idx, idx))
	}

	whereClause := ""
	for i, w := range where {
		if i > 0 {
			whereClause += " AND "
		}
		whereClause += w
	}
	return whereClause, args
}

// isOpenTab: a POS tab listing orders still being worked on.
func isOpenTab(filterType string) bool {
	switch filterType {
	case ordersapi.OrderFilterTypeActive, ordersapi.OrderFilterTypeTakeaway, ordersapi.OrderFilterTypeDelivery,
		ordersapi.OrderFilterTypePickup, ordersapi.OrderFilterTypeOnline:
		return true
	}
	return false
}

// asksScheduled: the till's Scheduled sub-tab sends status=scheduled.
func asksScheduled(statuses []string) bool {
	return len(statuses) == 1 && statuses[0] == ordersapi.StatusScheduled
}

func splitFilterValues(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		v := ordersapi.NormalizeStatus(part)
		if v == "" || v == "all" {
			continue
		}
		out = append(out, v)
	}
	return out
}
