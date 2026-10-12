package order

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"bakehouse/pkg/timex"
	"bakehouse/services/orders/internal/store/shared"
	"bakehouse/services/orders/ordersapi"
)

// UpdateOrderStatus moves an order to status and records the event.
//
// It returns the status the order was in *before* the write. Callers use that
// to tell a real transition from a repeat: staff marking an already-delivered
// order as delivered again must not fire the side effects a first transition
// does — the popularity rollup in particular, which would otherwise count the
// same order's products twice.
func (s *Repo) UpdateOrderStatus(ctx context.Context, orderID, status, actorType string, actorID *string, note string) (string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	// The previous status is read inside the same statement rather than as a
	// separate SELECT, so nothing can change it between the read and the
	// write.
	var previous string
	err = tx.QueryRow(ctx, `
		WITH prev AS (SELECT status FROM (SELECT * FROM orders WHERE deleted_at IS NULL) orders WHERE id = $1)
		UPDATE orders SET status = $2, updated_at = NOW(),
			-- Who handed the goods back is only meaningful on a return. It used
			-- to be stamped on 'fulfilled' too, so every completed order showed a
			-- "returned by" that never happened.
			returned_by = CASE WHEN $2 = 'returned' THEN COALESCE($3, returned_by) ELSE returned_by END,
			settled_by = CASE WHEN $2 = 'settled' THEN COALESCE($3, settled_by) ELSE settled_by END,
			order_deliver_date = CASE WHEN $2 = 'fulfilled' AND order_deliver_date IS NULL THEN COALESCE($4::timestamptz, NOW())::date ELSE order_deliver_date END,
			order_deliver_time = CASE WHEN $2 = 'fulfilled' AND order_deliver_time IS NULL THEN COALESCE($4::timestamptz, NOW())::time ELSE order_deliver_time END,
			total_duration_secs = CASE
				WHEN ($2 = 'settled' OR $2 = 'fulfilled') AND total_duration_secs IS NULL
				THEN EXTRACT(EPOCH FROM COALESCE($4::timestamptz, NOW()) - created_at)::INTEGER
				ELSE total_duration_secs
			END,
			-- The end is the first closing state the order reaches, and stays.
			end_time = CASE
				WHEN $2 IN ('settled', 'fulfilled', 'cancelled', 'returned') THEN COALESCE(end_time, $4::timestamptz, NOW())
				ELSE end_time
			END,
			-- KDS: a cancelled order is a cancelled ticket, so the kitchen sees
			-- it cancelled rather than still queued.
			kot_status = CASE WHEN $2 = 'cancelled' THEN 'cancelled' ELSE kot_status END
		WHERE id = $1
		RETURNING (SELECT status FROM prev)`, orderID, status, actorID, timex.EventTimeFrom(ctx)).Scan(&previous)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ordersapi.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	// The event is stamped with the moment the caller said it happened
	// (statusAt on the request), or now.
	if _, err := tx.Exec(ctx, `INSERT INTO order_status_events (order_id, status, actor_id, actor_type, note, created_at)
		VALUES ($1,$2,$3,$4,$5, COALESCE($6::timestamptz, NOW()))`,
		orderID, status, actorID, actorType, shared.NullIfEmpty(note), timex.EventTimeFrom(ctx)); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return previous, nil
}

func (s *Repo) UpdatePaymentStatus(ctx context.Context, orderID, paymentStatus string) error {
	_, err := s.pool.Exec(ctx, `UPDATE orders SET payment_status = $2, updated_at = NOW() WHERE id = $1`, orderID, paymentStatus)
	return err
}

// ClaimOrder links a previously-guest order to a customer account (ACC-14).
func (s *Repo) ClaimOrder(ctx context.Context, orderID, customerID string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE orders SET customer_id = $2, updated_at = NOW() WHERE id = $1 AND customer_id IS NULL`, orderID, customerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ordersapi.ErrNotFound
	}
	return nil
}

func (s *Repo) SetOrderNotes(ctx context.Context, orderID, notes string) error {
	_, err := s.pool.Exec(ctx, `UPDATE orders SET notes = $2, updated_at = NOW() WHERE id = $1`, orderID, notes)
	return err
}

// SetUpdatedBiller records which staff member last touched the order from the
// till. Called by every POS mutation (update, split, return, payment) with the
// staff id from the token; a nil id is a no-op.
func (s *Repo) SetUpdatedBiller(ctx context.Context, orderID string, staffID *string) error {
	if staffID == nil || strings.TrimSpace(*staffID) == "" {
		return nil
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE orders SET updated_biller_id = $2, updated_at = NOW() WHERE id = $1 AND deleted_at IS NULL`,
		orderID, strings.TrimSpace(*staffID))
	return err
}

func (s *Repo) UpdateOrderExtraFields(ctx context.Context, orderID string, customerID *string, clearCustomer bool, name, mobile, address, email, deliverDate, deliverTime, city, postcode, street, state *string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE orders SET
			customer_id = CASE
				WHEN $9::boolean THEN NULL
				ELSE COALESCE(NULLIF($2, '')::uuid, customer_id)
			END,
			-- Detaching also wipes the purchaser snapshot copied from that customer,
			-- otherwise the GET keeps showing their name and phone with customerId null.
			purchaser_first_name = CASE
				WHEN $9::boolean THEN ''
				WHEN NULLIF($2, '') IS NOT NULL AND (SELECT first_name FROM customers WHERE id::text = $2) IS NOT NULL
					THEN (SELECT first_name FROM customers WHERE id::text = $2)
				ELSE purchaser_first_name END,
			purchaser_last_name = CASE
				WHEN $9::boolean THEN ''
				WHEN NULLIF($2, '') IS NOT NULL AND (SELECT last_name FROM customers WHERE id::text = $2) IS NOT NULL
					THEN (SELECT last_name FROM customers WHERE id::text = $2)
				ELSE purchaser_last_name END,
			purchaser_email = CASE
				WHEN $9::boolean THEN ''
				WHEN NULLIF($2, '') IS NOT NULL AND (SELECT email FROM customers WHERE id::text = $2) IS NOT NULL
					THEN (SELECT email FROM customers WHERE id::text = $2)
				ELSE purchaser_email END,
			purchaser_phone = CASE
				WHEN $9::boolean THEN NULL
				WHEN NULLIF($2, '') IS NOT NULL AND (SELECT mobile_number FROM customers WHERE id::text = $2) IS NOT NULL
					THEN (SELECT mobile_number FROM customers WHERE id::text = $2)
				ELSE purchaser_phone END,
			guest_customer_name = COALESCE($3, guest_customer_name),
			guest_customer_mobile = COALESCE($4, guest_customer_mobile),
			guest_customer_address = COALESCE($5, guest_customer_address),
			guest_customer_email = COALESCE($6, guest_customer_email),
			guest_customer_city = COALESCE($10, guest_customer_city),
			guest_customer_postcode = COALESCE($11, guest_customer_postcode),
			guest_customer_street = COALESCE($12, guest_customer_street),
			guest_customer_state = COALESCE($13, guest_customer_state),
			order_deliver_date = COALESCE(NULLIF($7, '')::date, order_deliver_date),
			order_deliver_time = COALESCE(NULLIF($8, '')::time, order_deliver_time),
			updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL`,
		orderID, shared.NilIfEmptyPtr(customerID), name, mobile, address, email, deliverDate, deliverTime, clearCustomer, city, postcode, street, state)
	return err
}

// RecordCashCollected settles a cash-on-delivery order.
//
// Guarded on the current state rather than blindly updating: taking the money
// twice is the mistake this is protecting against, and a driver marking an
// order collected while the counter is doing the same is exactly how it
// happens. RowsAffected of zero means someone else got there first, which the
// caller reports as a conflict rather than a success.
func (s *Repo) RecordCashCollected(ctx context.Context, orderID, staffID string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE orders
		   SET payment_status = 'captured',
		       cash_collected_at = COALESCE($3::timestamptz, NOW()),
		       cash_collected_by = $2,
		       -- Whoever collected the cash settled the order.
		       settled_by = COALESCE(settled_by, $2::text),
		       updated_at = NOW()
		 WHERE id = $1
		   AND payment_method = 'cash_on_delivery'
		   AND payment_status = 'pending'`, orderID, staffID, timex.EventTimeFrom(ctx))
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	// The payment row is the financial record; the order column is the
	// denormalised copy the lists read. Both move together.
	_, err = s.pool.Exec(ctx, `
		UPDATE payments SET status = 'captured', captured_at = COALESCE($2::timestamptz, NOW())
		 WHERE order_id = $1 AND method = 'cash' AND status = 'pending'`, orderID, timex.EventTimeFrom(ctx))
	return true, err
}

// SoftDeleteOrder hides an order everywhere without losing it.
//
// For a till entry that should never have existed. Distinct from cancelling,
// which is a state the customer is meant to see.
func (s *Repo) SoftDeleteOrder(ctx context.Context, orderID string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE orders SET deleted_at = NOW(), updated_at = NOW()
		  WHERE id = $1 AND deleted_at IS NULL`, orderID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ordersapi.ErrNotFound
	}
	return nil
}

// SetOrderFulfilment changes how the order leaves the store (pickup,
// delivery or takeaway).
func (s *Repo) SetOrderFulfilment(ctx context.Context, orderID, fulfilmentType string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE orders SET fulfilment_type = $2, updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL`, orderID, fulfilmentType)
	return err
}
