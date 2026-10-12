package kds

import (
	"context"
	"time"

	"bakehouse/services/orders/internal/store/order"
)

// ReleaseToKitchenIfAhead marks a later-dated order as started early. An
// order due today or earlier is on the board already and is left alone.
// Reports whether the order was released.
func (s *Repo) ReleaseToKitchenIfAhead(ctx context.Context, orderID string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE orders SET kds_released_at = COALESCE(kds_released_at, NOW()), updated_at = NOW()
		 WHERE id = $1 AND deleted_at IS NULL
		   AND scheduled_date > `+order.BranchTodaySQL, orderID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// recalledOrderIDs is which of these orders the KDS recalled (and when).
func (s *Repo) recalledOrderIDs(ctx context.Context, ids []string) (map[string]time.Time, error) {
	out := map[string]time.Time{}
	rows, err := s.pool.Query(ctx,
		`SELECT id::text, kds_recalled_at FROM orders WHERE id::text = ANY($1) AND kds_recalled_at IS NOT NULL`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var at time.Time
		if err := rows.Scan(&id, &at); err != nil {
			return nil, err
		}
		out[id] = at
	}
	return out, rows.Err()
}

// releasedOrderIDs is which of these orders were started early.
func (s *Repo) releasedOrderIDs(ctx context.Context, ids []string) (map[string]time.Time, error) {
	out := map[string]time.Time{}
	rows, err := s.pool.Query(ctx,
		`SELECT id::text, kds_released_at FROM orders WHERE id::text = ANY($1) AND kds_released_at IS NOT NULL`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var at time.Time
		if err := rows.Scan(&id, &at); err != nil {
			return nil, err
		}
		out[id] = at
	}
	return out, rows.Err()
}

// DueScheduledOrderIDs is the orders still marked scheduled whose day has
// come at their store.
func (s *Repo) DueScheduledOrderIDs(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text FROM orders
		 WHERE deleted_at IS NULL AND status = 'scheduled'
		   AND scheduled_date <= `+order.BranchTodaySQL+`
		 LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
