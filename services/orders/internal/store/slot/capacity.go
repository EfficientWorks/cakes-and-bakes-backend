package slot

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/orders/ordersapi"
)

// ReserveSlot atomically increments reserved_count if capacity remains,
// using SELECT ... FOR UPDATE inside the caller's transaction so concurrent
// checkouts cannot oversell a slot (SCH-04, NFR-05).
func ReserveSlot(ctx context.Context, tx pgx.Tx, slotID string) error {
	var maxCap, reserved int
	err := tx.QueryRow(ctx, `SELECT max_capacity, reserved_count FROM slot_capacity WHERE id = $1 FOR UPDATE`, slotID).Scan(&maxCap, &reserved)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ordersapi.ErrNotFound
		}
		return err
	}
	if reserved >= maxCap {
		return ordersapi.ErrSlotFull
	}
	_, err = tx.Exec(ctx, `UPDATE slot_capacity SET reserved_count = reserved_count + 1 WHERE id = $1`, slotID)
	return err
}

// ReleaseSlot decrements reserved_count (amendment/cancellation releasing
// capacity — ORD-09, ORD-16).
func ReleaseSlot(ctx context.Context, tx pgx.Tx, slotID string) error {
	_, err := tx.Exec(ctx, `UPDATE slot_capacity SET reserved_count = GREATEST(reserved_count - 1, 0) WHERE id = $1`, slotID)
	return err
}

func (s *Repo) UpdateSlotCapacity(ctx context.Context, id string, maxCapacity int) error {
	tag, err := s.pool.Exec(ctx, `UPDATE slot_capacity SET max_capacity = $2 WHERE id = $1`, id, maxCapacity)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ordersapi.ErrNotFound
	}
	return nil
}
