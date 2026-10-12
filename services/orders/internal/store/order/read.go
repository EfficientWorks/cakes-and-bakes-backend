package order

import (
	"context"
	"encoding/json"
	"strings"

	"bakehouse/pkg/tenant"
	"bakehouse/services/orders/ordersapi"
)

func (s *Repo) GetOrderByID(ctx context.Context, id string) (*ordersapi.Order, error) {
	o, err := scanOrder(s.pool.QueryRow(ctx, SelectSQL+" WHERE id = $1", id))
	if err != nil {
		return nil, err
	}
	// Another business's order is not found, not forbidden: its existence is
	// not this caller's business either.
	if !tenant.OwnsRow(ctx, o.BusinessID) {
		return nil, ordersapi.ErrNotFound
	}
	return s.attachOrderChildren(ctx, o)
}

func (s *Repo) GetOrderByReference(ctx context.Context, reference string) (*ordersapi.Order, error) {
	o, err := scanOrder(s.pool.QueryRow(ctx, SelectSQL+" WHERE reference = $1", reference))
	if err != nil {
		return nil, err
	}
	if !tenant.OwnsRow(ctx, o.BusinessID) {
		return nil, ordersapi.ErrNotFound
	}
	return s.attachOrderChildren(ctx, o)
}

// FindOrderByIdempotencyKey is the public form used by the service layer to
// short-circuit CreateOrder before it even resolves a cart — the cart may
// already be gone (checkout deletes it on success), so a client retrying
// after a network timeout must be answered from the idempotency key alone,
// not from cart state (SF-28).
func (s *Repo) FindOrderByIdempotencyKey(ctx context.Context, key string) (*ordersapi.Order, error) {
	id, err := s.findOrderIDByIdempotencyKey(ctx, key)
	if err != nil {
		return nil, ordersapi.ErrNotFound
	}
	return s.GetOrderByID(ctx, id)
}

func (s *Repo) findOrderIDByIdempotencyKey(ctx context.Context, key string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `SELECT id FROM (SELECT * FROM orders WHERE deleted_at IS NULL) orders WHERE idempotency_key = $1`, key).Scan(&id)
	return id, err
}

// OrderCustomerID returns the customer an order is linked to, or "" for a
// walk-in.
func (s *Repo) OrderCustomerID(ctx context.Context, orderID string) string {
	var cid *string
	if err := s.pool.QueryRow(ctx,
		`SELECT customer_id::text FROM orders WHERE id = $1 AND deleted_at IS NULL`, orderID).Scan(&cid); err != nil || cid == nil {
		return ""
	}
	return *cid
}

// GetCustomerDetails returns customer fields from customers and customer_addresses.
func (s *Repo) GetCustomerDetails(ctx context.Context, customerID string) *CustomerDetails {
	if strings.TrimSpace(customerID) == "" {
		return nil
	}
	var fnStr, lnStr, emStr, mobStr, dobStr, annivStr, addrStr, cityStr, postStr, streetStr, stateStr string
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(c.first_name, ''),
		       COALESCE(c.last_name, ''),
		       COALESCE(c.email, ''),
		       COALESCE(c.mobile_number, ''),
		       COALESCE(c.date_of_birth::text, ''),
		       COALESCE(c.anniversary_date::text, ''),
		       COALESCE(NULLIF(ca.address, ''), ca.street, ''),
		       COALESCE(ca.city, ''),
		       COALESCE(ca.postcode, ''),
		       COALESCE(ca.street, ''),
		       COALESCE(ca.state, '')
		FROM customers c
		LEFT JOIN LATERAL (
			SELECT address, street, city, state, postcode
			FROM customer_addresses
			WHERE customer_id = c.id
			ORDER BY is_default DESC, created_at DESC
			LIMIT 1
		) ca ON true
		WHERE c.id::text = $1 AND c.deleted_at IS NULL`, strings.TrimSpace(customerID)).
		Scan(&fnStr, &lnStr, &emStr, &mobStr, &dobStr, &annivStr, &addrStr, &cityStr, &postStr, &streetStr, &stateStr)
	if err != nil {
		return nil
	}
	d := &CustomerDetails{}
	if fnStr != "" {
		d.FirstName = &fnStr
	}
	if lnStr != "" {
		d.LastName = &lnStr
	}
	if emStr != "" {
		d.Email = &emStr
	}
	if mobStr != "" {
		d.MobileNumber = &mobStr
	}
	if dobStr != "" {
		d.DOB = &dobStr
	}
	if annivStr != "" {
		d.AnniversaryDate = &annivStr
	}
	if addrStr != "" {
		d.Address = &addrStr
	}
	if cityStr != "" {
		d.City = &cityStr
	}
	if postStr != "" {
		d.Postcode = &postStr
	}
	if streetStr != "" {
		d.Street = &streetStr
	}
	if stateStr != "" {
		d.State = &stateStr
	}
	return d
}

// GetCustomerAddress returns the address, city and postcode from the
// customer's default (or most recent) customer_addresses row. All three
// pointers are nil when no row exists or the customer ID is blank.
func (s *Repo) GetCustomerAddress(ctx context.Context, customerID string) (address, city, postcode *string) {
	if strings.TrimSpace(customerID) == "" {
		return nil, nil, nil
	}
	var addrStr, cityStr, postStr string
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(NULLIF(address, ''), street, ''),
		       COALESCE(NULLIF(city, ''), ''),
		       COALESCE(NULLIF(postcode, ''), '')
		FROM customer_addresses
		WHERE customer_id = $1
		ORDER BY is_default DESC, created_at DESC
		LIMIT 1`, strings.TrimSpace(customerID)).
		Scan(&addrStr, &cityStr, &postStr)
	if err != nil {
		return nil, nil, nil
	}
	// if addrStr != "" {
	address = &addrStr
	// }
	// if cityStr != "" {
	city = &cityStr
	// }
	// if postStr != "" {
	postcode = &postStr
	// }
	return address, city, postcode
}

func (s *Repo) ListOrderAmendments(ctx context.Context, orderID string) ([]ordersapi.OrderAmendment, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, order_id, actor_id, actor_role, reason, before_value, after_value, price_delta_cents, created_at
		FROM order_amendments WHERE order_id = $1 ORDER BY created_at`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ordersapi.OrderAmendment
	for rows.Next() {
		var a ordersapi.OrderAmendment
		var beforeRaw, afterRaw []byte
		if err := rows.Scan(&a.ID, &a.OrderID, &a.ActorID, &a.ActorRole, &a.Reason, &beforeRaw, &afterRaw, &a.PriceDeltaCents, &a.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(beforeRaw, &a.BeforeValue)
		_ = json.Unmarshal(afterRaw, &a.AfterValue)
		out = append(out, a)
	}
	return out, rows.Err()
}
