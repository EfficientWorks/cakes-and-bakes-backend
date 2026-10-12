package report

/*
 * What counts as a sale on the reports.
 *
 * A settled order is one that was handed over AND paid for: fulfilled, with
 * its payment captured (a partial refund is still a captured payment — the
 * lines that were kept were paid for). An order that is merely fulfilled but
 * still owed, or one that was cancelled or fully returned, is not a sale and
 * must not feed the sales, tender or best-seller figures. Those states are
 * reported on their own lines.
 */
const settledOrderSQL = `o.status = 'fulfilled' AND lower(o.payment_status) IN ('captured', 'paid', 'partial_refund')`

// A line that still counts: not cancelled at the till and not fully handed
// back. Quantity and money are net of returns, the same arithmetic the order
// total uses (see domain.recalculatePOSTotals).
const liveLineSQL = `COALESCE(oi.production_status, 'pending') NOT IN ('cancelled', 'cancel', 'returned', 'return')`

const keptQtySQL = `(oi.quantity - COALESCE(oi.returned_quantity, 0))`

const keptAmountSQL = `(oi.unit_price_cents * (oi.quantity - COALESCE(oi.returned_quantity, 0)) - COALESCE(oi.discount_cents, 0))`

// What a returned order came to, tip excluded: goods, tax and delivery net
// of discount. The tip stays with the staff whatever became of the order.
//
// A fully returned order has had its totals recomputed from the kept lines,
// which is none of them, so subtotal, discount and tax are already zero on
// the row and total_cents holds no more than delivery and service. The value
// is rebuilt from the lines instead: what was handed back at its unit price,
// less the line and order discounts, plus tax at the order's rate, plus
// whatever the row still carries (delivery, service, or a tax the till set
// by hand). A row whose subtotal survived — nothing was recomputed — is read
// as it stands.
const returnedOrderAmountSQL = `(CASE
	WHEN o.subtotal_cents > 0 THEN (o.total_cents - COALESCE(o.tip_cents, 0))
	ELSE (
		(SELECT ret.net
		        + CASE WHEN COALESCE(o.tax_pct, 0) > 0 THEN ROUND(ret.net * o.tax_pct / 100)::bigint ELSE 0 END
		   FROM (
		     SELECT GREATEST(g.goods
		              - CASE WHEN COALESCE(o.discount_pct, 0) > 0
		                     THEN ROUND(g.goods * LEAST(o.discount_pct, 100) / 100)::bigint
		                     ELSE 0 END, 0) AS net
		       FROM (
		         SELECT COALESCE(SUM(oi.unit_price_cents * COALESCE(oi.returned_quantity, 0)
		                             - COALESCE(oi.discount_cents, 0)), 0)::bigint AS goods
		           FROM order_items oi
		          WHERE oi.order_id = o.id
		       ) g
		   ) ret)
		+ (o.total_cents - COALESCE(o.tip_cents, 0))
	)
END)`

// When the order happened, for bucketing by day and hour: the time the till
// or storefront said it was placed (start_time), not the server clock the row
// was written on. An order keyed in at 4PM used to land in the 7PM bucket
// when the request arrived later, or from a server in another zone.
const orderTimeSQL = `COALESCE(o.start_time, o.created_at)`

// What a line's unit costs before its add-ons: the variation-priced product
// alone. Add-ons are separate goods, and the best-seller list is ranked on
// the product, not on what was bolted onto it.
const unitPriceExAddonsSQL = `(oi.unit_price_cents - COALESCE((
	SELECT SUM(COALESCE((a->>'priceDelta')::bigint, 0))
	  FROM jsonb_array_elements(CASE WHEN jsonb_typeof(oi.selected_addons) = 'array' THEN oi.selected_addons ELSE '[]'::jsonb END) a), 0))`

const keptAmountExAddonsSQL = `(` + unitPriceExAddonsSQL + ` * (oi.quantity - COALESCE(oi.returned_quantity, 0)) - COALESCE(oi.discount_cents, 0))`
