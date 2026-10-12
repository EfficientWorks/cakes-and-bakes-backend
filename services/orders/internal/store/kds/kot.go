package kds

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"bakehouse/pkg/timex"
	"bakehouse/services/orders/internal/store/shared"
	"bakehouse/services/orders/ordersapi"
)

func insertKOTEvent(ctx context.Context, tx pgx.Tx, orderID string, itemID *string, from *string, to string, a KOTActor) error {
	src := a.Source
	if src == "" {
		src = "kds"
	}
	typ := a.Type
	if typ == "" {
		typ = "staff"
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO order_kot_events (order_id, order_item_id, from_status, to_status, actor_id, actor_type, source, note, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8, COALESCE($9::timestamptz, NOW()))`,
		orderID, itemID, from, to, a.ID, typ, src, shared.NullIfEmpty(a.Note), timex.EventTimeFrom(ctx))
	return err
}

/*
 * MoveOrderKOT moves an order's kitchen status from `from` to `to`, together
 * with the lines that move with it, and writes the kitchen timeline — one
 * transaction, conditional on `from` so a stale screen cannot overwrite a
 * fresher one.
 *
 *   → preparing   lines still queued start (production_status in_progress)
 *   → queue       (undo) lines in preparation go back to the queue
 *   → ready       every line still standing is ready (production_status done)
 */
func (s *Repo) MoveOrderKOT(ctx context.Context, orderID, from, to string, a KOTActor) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	at := timex.EventTimeFrom(ctx)

	tag, err := tx.Exec(ctx, `
		UPDATE orders SET
			kot_status       = $3,
			kot_started_at   = CASE WHEN $3 = 'preparing' THEN COALESCE(kot_started_at, $4::timestamptz, NOW())
			                        WHEN $3 = 'queue' THEN NULL ELSE kot_started_at END,
			kot_ready_at     = CASE WHEN $3 = 'ready' THEN COALESCE($4::timestamptz, NOW())
			                        WHEN $3 IN ('queue', 'preparing') THEN NULL ELSE kot_ready_at END,
			kot_completed_at = CASE WHEN $3 IN ('out_for_delivery', 'delivered', 'completed') THEN COALESCE($4::timestamptz, NOW())
			                        WHEN $3 = 'ready' THEN NULL ELSE kot_completed_at END,
			-- Recalled: a handed-over order brought back to ready. Cleared once
			-- it is handed over again.
			kds_recalled_at  = CASE WHEN $2 IN ('out_for_delivery', 'delivered', 'completed') AND $3 = 'ready'
			                        THEN COALESCE($4::timestamptz, NOW())
			                        WHEN $3 IN ('out_for_delivery', 'delivered', 'completed') THEN NULL
			                        ELSE kds_recalled_at END,
			updated_at       = NOW()
		 WHERE id = $1 AND kot_status = $2 AND deleted_at IS NULL`, orderID, from, to, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrKOTConflict
	}

	switch to {
	case ordersapi.KOTPreparing:
		if _, err := tx.Exec(ctx, `
			UPDATE order_items SET
				kot_status = 'preparing',
				kot_started_at = COALESCE(kot_started_at, $2::timestamptz, NOW()),
				production_status = CASE WHEN production_status = 'pending' THEN 'in_progress' ELSE production_status END,
				production_started_at = COALESCE(production_started_at, $2::timestamptz, NOW())
			 WHERE order_id = $1 AND kot_status = 'queue'
			   AND production_status NOT IN ('cancelled', 'cancel', 'returned')`, orderID, at); err != nil {
			return err
		}
	case ordersapi.KOTQueue:
		if _, err := tx.Exec(ctx, `
			UPDATE order_items SET kot_status = 'queue', kot_started_at = NULL,
				production_status = CASE WHEN production_status = 'in_progress' THEN 'pending' ELSE production_status END
			 WHERE order_id = $1 AND kot_status = 'preparing'`, orderID); err != nil {
			return err
		}
	case ordersapi.KOTReady:
		if from == ordersapi.KOTPreparing || from == ordersapi.KOTQueue {
			if _, err := tx.Exec(ctx, `
				UPDATE order_items SET
					kot_status = 'ready',
					kot_started_at = COALESCE(kot_started_at, $2::timestamptz, NOW()),
					kot_ready_at = COALESCE(kot_ready_at, $2::timestamptz, NOW()),
					production_status = CASE WHEN production_status IN ('pending', 'in_progress') THEN 'done' ELSE production_status END,
					production_done_at = COALESCE(production_done_at, $2::timestamptz, NOW())
				 WHERE order_id = $1 AND kot_status IN ('queue', 'preparing')
				   AND production_status NOT IN ('cancelled', 'cancel', 'returned')`, orderID, at); err != nil {
				return err
			}
		}
	}

	f := from
	if err := insertKOTEvent(ctx, tx, orderID, nil, &f, to, a); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

/*
 * MoveItemKOT moves one line, conditional on its status, and — if this is
 * the first line the kitchen starts — moves a queued order to preparing too.
 * Returns whether the order moved.
 */
func (s *Repo) MoveItemKOT(ctx context.Context, orderID, itemID, from, to string, a KOTActor) (orderStarted bool, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	at := timex.EventTimeFrom(ctx)

	tag, err := tx.Exec(ctx, `
		UPDATE order_items SET
			kot_status = $4,
			kot_started_at = CASE WHEN $4 = 'preparing' THEN COALESCE(kot_started_at, $5::timestamptz, NOW())
			                      WHEN $4 = 'queue' THEN NULL ELSE kot_started_at END,
			kot_ready_at   = CASE WHEN $4 IN ('ready', 'completed') THEN COALESCE($5::timestamptz, NOW()) ELSE NULL END,
			production_status = CASE $4 WHEN 'preparing' THEN 'in_progress'
			                            WHEN 'ready' THEN 'done'
			                            WHEN 'completed' THEN 'done'
			                            WHEN 'queue' THEN 'pending'
			                            ELSE production_status END,
			production_started_at = CASE WHEN $4 IN ('preparing', 'ready') THEN COALESCE(production_started_at, $5::timestamptz, NOW()) ELSE production_started_at END,
			production_done_at    = CASE WHEN $4 IN ('ready', 'completed') THEN COALESCE($5::timestamptz, NOW()) ELSE production_done_at END
		 WHERE id = $1 AND order_id = $2 AND kot_status = $3`, itemID, orderID, from, to, at)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, ErrKOTConflict
	}
	f := from
	if err := insertKOTEvent(ctx, tx, orderID, &itemID, &f, to, a); err != nil {
		return false, err
	}

	if to == ordersapi.ItemKOTPreparing || to == ordersapi.ItemKOTReady {
		tag, err := tx.Exec(ctx, `
			UPDATE orders SET kot_status = 'preparing',
				kot_started_at = COALESCE(kot_started_at, $2::timestamptz, NOW()), updated_at = NOW()
			 WHERE id = $1 AND kot_status = 'queue'`, orderID, at)
		if err != nil {
			return false, err
		}
		if tag.RowsAffected() == 1 {
			orderStarted = true
			q := ordersapi.KOTQueue
			if err := insertKOTEvent(ctx, tx, orderID, nil, &q, ordersapi.KOTPreparing,
				KOTActor{ID: a.ID, Type: a.Type, Source: a.Source, Note: "Started with its first item"}); err != nil {
				return false, err
			}
		}
	}
	return orderStarted, tx.Commit(ctx)
}

// SetOrderKOTStatusRaw records a KOT status the POS/admin sent directly
// (frontend-driven), with its timeline event. Unconditional by design: the
// frontend is stating what the kitchen status is.
func (s *Repo) SetOrderKOTStatusRaw(ctx context.Context, orderID, to string, a KOTActor) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var from string
	if err := tx.QueryRow(ctx, `SELECT kot_status FROM orders WHERE id = $1 FOR UPDATE`, orderID).Scan(&from); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ordersapi.ErrNotFound
		}
		return err
	}
	if from == to {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE orders SET kot_status = $2, updated_at = NOW() WHERE id = $1`, orderID, to); err != nil {
		return err
	}
	if err := insertKOTEvent(ctx, tx, orderID, nil, &from, to, a); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// MarkAmended flags an order as changed while the kitchen had it. Re-flags an
// order the KDS had already acknowledged.
func (s *Repo) MarkAmended(ctx context.Context, orderID string, at *time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE orders SET is_amended = TRUE, amended_at = COALESCE($2::timestamptz, NOW()),
			amended_acknowledged = FALSE, amended_acknowledged_at = NULL, amended_acknowledged_by = NULL,
			updated_at = NOW()
		 WHERE id = $1`, orderID, at)
	return err
}

// AcknowledgeAmendment clears the Amended flag on the KDS.
func (s *Repo) AcknowledgeAmendment(ctx context.Context, orderID string, staffID *string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE orders SET amended_acknowledged = TRUE,
			amended_acknowledged_at = COALESCE($3::timestamptz, NOW()), amended_acknowledged_by = $2::uuid,
			updated_at = NOW()
		 WHERE id = $1 AND is_amended AND NOT amended_acknowledged`, orderID, staffID, timex.EventTimeFrom(ctx))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}
