package order

import (
	"fmt"
	"strings"
)

const SelectSQL = `
		SELECT id, reference, bill_no, branch_id, customer_id, channel, fulfilment_type,
	       recipient_first_name, recipient_last_name, recipient_phone, recipient_email, gift_message, gift_reason, is_gift,
	       purchaser_first_name, purchaser_last_name, purchaser_email, purchaser_phone,
	       delivery_street, delivery_city, delivery_state, delivery_postcode, delivery_instructions,
	       business_name, delivery_address_type,
	       photo_on_delivery, surprise_delivery,
	       scheduled_date, slot_start::text, slot_end::text, slot_capacity_id,
	       subtotal_cents, delivery_fee_cents, tax_cents, tip_cents, discount_cents, discount_code, discount_pct, tax_pct, service_charge_cents, total_cents, currency,
	       -- Null until the till takes the money; empty string on the wire.
	       status, payment_status, COALESCE(payment_method, ''), cash_collected_at, cash_collected_by, provider_payment_id, idempotency_key, marketing_opt_in, notes,
	       -- A cash-on-delivery order is settled by whoever collected the cash
	       COALESCE(settled_by, cash_collected_by::text) AS settled_by, returned_by, total_duration_secs, start_time, end_time,
	       (SELECT COALESCE(NULLIF(TRIM(CONCAT(first_name, ' ', last_name)), ''), email) FROM staff_users WHERE id::text = COALESCE(orders.settled_by, orders.cash_collected_by::text)) AS settled_by_name,
	       (SELECT COALESCE(NULLIF(TRIM(CONCAT(first_name, ' ', last_name)), ''), email) FROM staff_users WHERE id::text = orders.returned_by) AS returned_by_name,
	       biller_id, updated_biller_id,
	       (SELECT COALESCE(NULLIF(TRIM(CONCAT(first_name, ' ', last_name)), ''), email) FROM staff_users WHERE id::text = orders.biller_id) AS biller_name,
	       (SELECT COALESCE(NULLIF(TRIM(CONCAT(first_name, ' ', last_name)), ''), email) FROM staff_users WHERE id::text = orders.updated_biller_id) AS updated_biller_name,
	       split_mode,
	       guest_customer_name, guest_customer_mobile, guest_customer_address, guest_customer_email,
	       guest_customer_city, guest_customer_postcode, guest_customer_street, guest_customer_state,
	       order_deliver_date::text, order_deliver_time::text,
	       created_at, updated_at,
	       -- KDS
	       kot_status, kot_started_at, kot_ready_at, kot_completed_at,
	       is_amended, amended_at, amended_acknowledged, amended_acknowledged_at,
	       -- Order-based return totals (see 0076)
	       return_amount_cents, return_discount_pct, return_discount_cents,
	       return_tax_pct, return_tax_cents, return_total_cents,
	       -- When the order was fully paid: the last till payment or the cash collected or the card capture
	       CASE WHEN payment_status = 'captured' THEN COALESCE(
	           (SELECT max(bp.created_at) FROM bill_payments bp WHERE bp.order_id = orders.id),
	           cash_collected_at,
	           (SELECT max(p.captured_at) FROM payments p WHERE p.order_id = orders.id)) END AS settled_at,
	       -- Who cancelled it and why: the latest cancelled event on the timeline
	       CASE WHEN status = 'cancelled' THEN (
	           SELECT json_build_object(
	               'at', e.created_at, 'actorId', e.actor_id, 'actorType', e.actor_type, 'note', e.note,
	               'name', CASE e.actor_type
	                   WHEN 'staff' THEN (SELECT COALESCE(NULLIF(TRIM(CONCAT(su.first_name, ' ', su.last_name)), ''), su.email) FROM staff_users su WHERE su.id = e.actor_id)
	                   WHEN 'customer' THEN (SELECT COALESCE(NULLIF(TRIM(CONCAT(c.first_name, ' ', c.last_name)), ''), c.email) FROM customers c WHERE c.id = e.actor_id)
	               END)
	             FROM order_status_events e
	            WHERE e.order_id = orders.id AND e.status = 'cancelled'
	            ORDER BY e.created_at DESC LIMIT 1) END AS cancellation,
	       business_id
	FROM ` + liveOrders

/*
 * Every read goes through the orders that still exist.
 *
 * A deleted order is one a till rang up by mistake, and it has to be gone from
 * every list rather than most of them. Filtering in the FROM makes that
 * structural: a query cannot forget a WHERE clause it never had to write.
 *
 * Named rather than inlined because it is string-matched below — the item
 * count is bolted onto this same select — and a literal that appears twice is
 * a literal that will eventually appear differently.
 */
const liveOrders = `(SELECT * FROM orders WHERE deleted_at IS NULL) orders`

// orderListSelectWithCount is SelectSQL, aliased and with an item count.
//
// It is derived from SelectSQL rather than written out again. The two had
// drifted — the list was missing recipient_email, gift_reason,
// photo_on_delivery and surprise_delivery while its scan still expected them,
// so every call to ListOrders failed with a column mismatch and the admin
// order list returned INTERNAL_ERROR. Deriving it means the next column added
// to SelectSQL cannot desynchronise them again.
// No table alias: orders is the only table in the FROM, so every column name
// is already unambiguous, and not aliasing means the column list can be reused
// verbatim instead of being rewritten with a prefix.
// The FROM is matched through liveOrders rather than the literal "FROM
// orders": the select reads from the not-deleted subquery now, and a literal
// spelled out here would quietly stop matching it — which is exactly what
// happened, and it emptied the admin order list without erroring anywhere
// visible.
var orderListSelectWithCount = strings.Replace(
	SelectSQL,
	"\tFROM "+liveOrders,
	"\t       , (SELECT count(*) FROM order_items WHERE order_items.order_id = orders.id) AS item_count\n\tFROM "+liveOrders,
	1,
)

// BranchTodaySQL is the order's store's date now.
const BranchTodaySQL = `(NOW() AT TIME ZONE COALESCE((SELECT b.timezone FROM branches b WHERE b.id = orders.branch_id), 'America/New_York'))::date`

// orderPaidSQL: payment captured, or every split share paid.
const orderPaidSQL = `(lower(payment_status) IN ('captured', 'paid', 'partial_refund')
	OR (EXISTS (SELECT 1 FROM order_bills ob WHERE ob.order_id = orders.id)
	    AND NOT EXISTS (SELECT 1 FROM order_bills ob WHERE ob.order_id = orders.id AND ob.paid_cents < ob.amount_cents)))`

// kitchenHandedOverSQL: the KDS has finished with the order.
const kitchenHandedOverSQL = `lower(COALESCE(kot_status, '')) IN ('out_for_delivery', 'delivered', 'completed')`

// orderHistorySQL is the Order History tab.
const orderHistorySQL = `(status IN ('cancelled', 'returned', 'refunded')
	OR lower(payment_status) = 'refunded'
	OR (lower(channel) = 'web' AND ` + orderPaidSQL + ` AND (status = 'fulfilled' OR ` + kitchenHandedOverSQL + `))
	OR (lower(channel) <> 'web' AND (status = 'fulfilled'
	    OR (` + orderPaidSQL + ` AND (scheduled_date <= ` + BranchTodaySQL + ` OR ` + kitchenHandedOverSQL + `)))))`

// openTabSQL is an open (not history) order due today or earlier, or, with
// scheduled, due on a later day.
func openTabSQL(scheduled bool) string {
	cmp := "<="
	if scheduled {
		cmp = ">"
	}
	return "(NOT " + orderHistorySQL + " AND scheduled_date " + cmp + " " + BranchTodaySQL + ")"
}

// historyStatusSQL narrows Order History by the till's status filter. The
// till sends its own names (partialReturn; fulfilled for "Paid"); anything
// else is matched as an order status. argAt appends a value and returns its
// placeholder.
func historyStatusSQL(statuses []string, argAt func(v interface{}) string) string {
	var conds, plain []string
	for _, st := range statuses {
		switch strings.ReplaceAll(strings.ReplaceAll(st, "_", ""), "-", "") {
		case "cancelled", "canceled":
			conds = append(conds, "status = 'cancelled'")
		case "returned":
			conds = append(conds, "status IN ('returned', 'refunded')")
		case "partialreturn", "partialreturned":
			conds = append(conds, "(return_total_cents > 0 AND status NOT IN ('cancelled', 'returned', 'refunded'))")
		case "fulfilled", "paid", "completed":
			conds = append(conds, "status NOT IN ('cancelled', 'returned', 'refunded')")
		default:
			plain = append(plain, st)
		}
	}
	if len(plain) > 0 {
		conds = append(conds, fmt.Sprintf("lower(status) = ANY(%s)", argAt(plain)))
	}
	return "(" + strings.Join(conds, " OR ") + ")"
}
