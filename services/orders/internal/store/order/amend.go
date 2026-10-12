package order

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"bakehouse/pkg/timex"
	"bakehouse/services/orders/ordersapi"
)

// AmendOrderTx applies field changes to an order, optionally moves its slot
// reservation, adjusts totals, and writes the amendment record — all in one
// transaction so the order/slot/audit trail never diverge.
func (s *Repo) AmendOrderTx(ctx context.Context, orderID string, apply func(tx pgx.Tx, o *ordersapi.Order) error, amendment ordersapi.OrderAmendment) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	o, err := scanOrder(tx.QueryRow(ctx, SelectSQL+" WHERE id = $1 FOR UPDATE", orderID))
	if err != nil {
		return err
	}
	if !ordersapi.IsAmendable(o.Status) {
		return ErrAmendmentClosed
	}

	if err := apply(tx, o); err != nil {
		return err
	}

	before, _ := json.Marshal(amendment.BeforeValue)
	after, _ := json.Marshal(amendment.AfterValue)
	if _, err := tx.Exec(ctx, `
		INSERT INTO order_amendments (order_id, actor_id, actor_role, reason, before_value, after_value, price_delta_cents)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		orderID, amendment.ActorID, amendment.ActorRole, amendment.Reason, before, after, amendment.PriceDeltaCents); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO order_status_events (order_id, status, actor_id, actor_type, note, created_at)
		VALUES ($1, $2, $3, 'staff', $4, COALESCE($5::timestamptz, NOW()))`, orderID, o.Status, amendment.ActorID, "Amended: "+amendment.Reason, timex.EventTimeFrom(ctx)); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

var ErrAmendmentClosed = errors.New("order is already in production; amendment window closed")
