package pos

import (
	"context"
	"encoding/json"
	"time"

	"bakehouse/pkg/timex"
	"bakehouse/services/orders/internal/store/shared"
	"bakehouse/services/orders/ordersapi"
)

/*
 * Saving the till's view of an order's lines.
 *
 * The POS screen is where an order is worked on. It knows which round a line
 * is on, when it went to the kitchen, and whether the customer has since
 * changed their mind about it — and it knows all of that before the server
 * does, because the operator is making those decisions on the screen. So this
 * records what it is given rather than working anything out.
 *
 * A line with an id is one the screen already knows about, and is updated. A
 * line without one is new, and is inserted. Lines the screen does not mention
 * are left exactly as they are: an order being edited two rounds in should not
 * lose its first round because the screen only sent what changed.
 *
 * Nothing is deleted. A line the kitchen has already been handed cannot be
 * made to have never existed, so a cancelled line stays with its status set
 * and simply stops counting.
 */
func (s *Repo) UpsertPOSItems(ctx context.Context, orderID string, defaultBatchNo int, items []ordersapi.POSItem) error {
	if len(items) == 0 {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, it := range items {
		if it.ID == "" {
			// A new line joins the round the till named, or the next one if it
			// did not name one — which is what "send the rest to the kitchen"
			// means.
			batch := it.BatchNo
			if batch <= 0 {
				batch = defaultBatchNo
			}
			status := statusOrPending(it.Status)
			flavours, _ := json.Marshal(it.Flavours)
			addons, _ := json.Marshal(it.Addons)
			if _, err := tx.Exec(ctx, `
				INSERT INTO order_items (order_id, product_id, product_name, variant_id, variant_name,
					quantity, unit_price_cents, line_total_cents, selected_flavours, selected_addons,
					batch_no, discount_pct, discount_cents, production_status, send_kot_time, comments, cancelled_at,
					production_started_at, production_done_at, kot_status, gift_message, gift_reason)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,
					COALESCE(NULLIF($20, ''), CASE WHEN $14 IN ('cancel', 'cancelled') THEN 'cancelled' ELSE 'queue' END),
					$21, $22)`,
				orderID, shared.NilIfEmptyStr(it.ProductID), it.ProductName, shared.NilIfEmptyStr(it.VariantID), shared.NilIfEmptyStr(it.VariantName),
				it.Quantity, it.UnitPriceCents, it.LineTotalCents, flavours, addons,
				batch, it.DiscountPct, it.DiscountCents, status, it.SendKotTime, shared.NilIfEmptyStr(it.Comments), cancelTimeFor(ctx, it),
				productionStartedFor(ctx, it), productionDoneFor(ctx, it), it.KOTStatus,
				shared.NilIfEmptyPtr(it.GiftMessage), shared.NilIfEmptyPtr(it.GiftReason)); err != nil {
				return err
			}
			continue
		}

		/*
		 * An edit changes only what was sent.
		 *
		 * COALESCE on the optional columns, so a screen sending a status
		 * change does not blank the KOT time it did not mention. Quantity and
		 * the money are taken as given, because those are the numbers on the
		 * screen in front of the customer and a zero there is a real zero.
		 *
		 * Scoped by order_id as well as id: an item id from another order is
		 * not this order's to edit, and matching on the id alone would let one
		 * through.
		 */
		status := it.Status
		if _, err := tx.Exec(ctx, `
			UPDATE order_items
			   SET quantity          = CASE WHEN $3::int > 0 THEN $3 ELSE quantity END,
			       unit_price_cents  = CASE WHEN $4::bigint > 0 THEN $4 ELSE unit_price_cents END,
			       line_total_cents  = CASE WHEN $5::bigint > 0 THEN $5
			                                WHEN $3::int > 0 AND ($4::bigint > 0 OR unit_price_cents > 0)
			                                THEN $3 * (CASE WHEN $4::bigint > 0 THEN $4 ELSE unit_price_cents END)
			                                ELSE line_total_cents END,
			       batch_no          = CASE WHEN $6::int > 0 THEN $6 ELSE batch_no END,
			       production_status = CASE WHEN $7 <> '' THEN $7 ELSE production_status END,
			       returned_quantity = CASE WHEN $7 IN ('returned', 'return') THEN (CASE WHEN $3::int > 0 THEN $3 ELSE quantity END) ELSE returned_quantity END,
			       send_kot_time     = COALESCE($8, send_kot_time),
			       discount_pct      = COALESCE($9, discount_pct),
			       discount_cents    = CASE WHEN $10::bigint > 0 THEN $10
			                                WHEN $9 IS NOT NULL AND $9::numeric > 0 THEN ROUND((CASE WHEN $5::bigint > 0 THEN $5 ELSE line_total_cents END) * $9 / 100)
			                                WHEN $9 IS NOT NULL AND $9::numeric = 0 THEN 0
			                                ELSE discount_cents END,
			       -- Empty means the till did not mention the note; it does not clear it.
			       comments          = COALESCE($11::text, comments),
			       -- The cancel time the till sent; falls back to now when the
			       -- line is being cancelled without one, and is never cleared.
			       cancelled_at      = COALESCE($12::timestamptz, cancelled_at),
			       -- Production stamps: what the till sent, else the request's
			       -- event time when the status moves; never cleared.
			       production_started_at = COALESCE($13::timestamptz, production_started_at),
			       production_done_at    = COALESCE($14::timestamptz, production_done_at),
			       -- KDS: the kitchen status the frontend sent, else unchanged.
			       -- A line cancelled from the till is a cancelled line on the KDS too.
			       kot_status            = CASE WHEN $7 IN ('cancel', 'cancelled') THEN 'cancelled'
			                                    ELSE COALESCE(NULLIF($15, ''), kot_status) END,
			       -- Gift message / occasion: not sent keeps, "" clears.
			       gift_message          = CASE WHEN $16::text IS NULL THEN gift_message ELSE NULLIF($16, '') END,
			       gift_reason           = CASE WHEN $17::text IS NULL THEN gift_reason ELSE NULLIF($17, '') END
			 WHERE id = $1 AND order_id = $2`,
			it.ID, orderID, it.Quantity, it.UnitPriceCents, it.LineTotalCents,
			it.BatchNo, status, it.SendKotTime, it.DiscountPct, it.DiscountCents,
			shared.NilIfEmptyStr(it.Comments), cancelTimeFor(ctx, it),
			productionStartedFor(ctx, it), productionDoneFor(ctx, it), it.KOTStatus,
			shared.TrimPtr(it.GiftMessage), shared.TrimPtr(it.GiftReason)); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// cancelTimeFor is the cancel time to store for a line: what the till sent,
// or now for a line arriving as cancelled without one, so a cancelled line
// always says when.
func cancelTimeFor(ctx context.Context, it ordersapi.POSItem) *time.Time {
	if it.CancelTime != nil {
		return it.CancelTime
	}
	switch ordersapi.NormalizeItemStatus(it.Status) {
	case ordersapi.ItemCancel, ordersapi.ItemCancelled:
		now := timex.EventTimeOr(ctx, time.Now())
		return &now
	}
	return nil
}

// productionStartedFor: the time the till sent, else the request's event
// time (or now) when the line is moving to in_progress or done.
func productionStartedFor(ctx context.Context, it ordersapi.POSItem) *time.Time {
	if it.ProductionStartedAt != nil {
		return it.ProductionStartedAt
	}
	switch ordersapi.NormalizeItemStatus(it.Status) {
	case ordersapi.ItemInProgress, ordersapi.ItemDone:
		now := timex.EventTimeOr(ctx, time.Now())
		return &now
	}
	return nil
}

// productionDoneFor: the time the till sent, else the request's event time
// (or now) when the line is moving to done.
func productionDoneFor(ctx context.Context, it ordersapi.POSItem) *time.Time {
	if it.ProductionDoneAt != nil {
		return it.ProductionDoneAt
	}
	if ordersapi.NormalizeItemStatus(it.Status) == ordersapi.ItemDone {
		now := timex.EventTimeOr(ctx, time.Now())
		return &now
	}
	return nil
}

// statusOrPending defaults a line to not yet made.
func statusOrPending(s string) string {
	if s == "" {
		return ordersapi.ItemPending
	}
	return s
}
