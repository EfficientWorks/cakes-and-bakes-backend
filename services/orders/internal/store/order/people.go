package order

import (
	"context"

	"bakehouse/services/orders/internal/store/shared"
	"bakehouse/services/orders/ordersapi"
)

// UpsertCustomerAddress writes address, city and postcode onto the
// customer's default address row, creating it if there is none. A nil field
// keeps whatever the row already has, so a till that only knows the city
// does not blank the street.
// UpsertCustomerAddress writes the customer's default address; a nil field
// keeps what it has. Street falls back to the address line when only that is
// given.
func (s *Repo) UpsertCustomerAddress(ctx context.Context, customerID string, address, street, city, state, postcode *string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE customer_addresses SET
			address    = COALESCE($2, address),
			street     = COALESCE($5, $2, street),
			city       = COALESCE($3, city),
			state      = COALESCE($6, state),
			postcode   = COALESCE($4, postcode),
			updated_at = NOW()
		WHERE id = (
			SELECT id FROM customer_addresses
			 WHERE customer_id = $1
			 ORDER BY is_default DESC, created_at DESC
			 LIMIT 1
		)`, customerID, address, city, postcode, street, state)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO customer_addresses (customer_id, label, address, street, city, state, postcode, country, is_default)
		VALUES ($1, 'Default', COALESCE($2, ''), COALESCE($5, $2, ''), COALESCE($3, ''), COALESCE($6, ''), COALESCE($4, ''), 'US', true)`,
		customerID, address, city, postcode, street, state)
	return err
}

// SetOrderCustomerAddress writes the order's copy of its customer's address
// exactly as given; a blank field is stored as null.
func (s *Repo) SetOrderCustomerAddress(ctx context.Context, orderID, address, street, city, state, postcode string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE orders SET
			guest_customer_address = NULLIF($2, ''), guest_customer_street = NULLIF($3, ''),
			guest_customer_city = NULLIF($4, ''), guest_customer_state = NULLIF($5, ''),
			guest_customer_postcode = NULLIF($6, ''), updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL`, orderID, address, street, city, state, postcode)
	return err
}

// SetCustomerAddress writes a customer's address exactly as given: a blank
// field is saved blank rather than keeping the old value or being filled
// from another field. Used when a till order carries the customer's details,
// so what the order sent is what the order reads back.
func (s *Repo) SetCustomerAddress(ctx context.Context, customerID, address, street, city, state, postcode string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE customer_addresses SET
			address = $2, street = $3, city = $4, state = $5, postcode = $6, updated_at = NOW()
		WHERE id = (
			SELECT id FROM customer_addresses
			 WHERE customer_id = $1
			 ORDER BY is_default DESC, created_at DESC
			 LIMIT 1
		)`, customerID, address, street, city, state, postcode)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	if address == "" && street == "" && city == "" && state == "" && postcode == "" {
		return nil
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO customer_addresses (customer_id, label, address, street, city, state, postcode, country, is_default)
		VALUES ($1, 'Default', $2, $3, $4, $5, $6, 'US', true)`,
		customerID, address, street, city, state, postcode)
	return err
}

// SetOrderPurchaserContact writes the purchaser's name, email and phone onto
// an order that has no linked customer, so the details the till typed are
// kept on the order. A nil or blank field keeps what the order has.
func (s *Repo) SetOrderPurchaserContact(ctx context.Context, orderID string, firstName, lastName, email, phone *string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE orders SET
			purchaser_first_name = COALESCE($2, purchaser_first_name),
			purchaser_last_name  = COALESCE($3, purchaser_last_name),
			purchaser_email      = COALESCE($4, purchaser_email),
			purchaser_phone      = COALESCE($5, purchaser_phone),
			updated_at = NOW()
		 WHERE id = $1 AND customer_id IS NULL AND deleted_at IS NULL`,
		orderID, shared.NilIfEmptyPtr(firstName), shared.NilIfEmptyPtr(lastName), shared.NilIfEmptyPtr(email), shared.NilIfEmptyPtr(phone))
	return err
}

// UpdateOrderRecipient sets who the order is for. A nil field keeps the
// order's own value.
func (s *Repo) UpdateOrderRecipient(ctx context.Context, orderID string, r ordersapi.POSRecipient) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE orders SET
			recipient_first_name = COALESCE($2, recipient_first_name),
			recipient_last_name  = COALESCE($3, recipient_last_name),
			-- Sent blank clears the phone or email; not sent keeps it.
			recipient_phone      = CASE WHEN $4::text IS NULL THEN recipient_phone ELSE NULLIF($4, '') END,
			recipient_email      = CASE WHEN $5::text IS NULL THEN recipient_email ELSE NULLIF($5, '') END,
			-- The recipient's address is the order's delivery address.
			delivery_street      = CASE WHEN $6::text IS NULL THEN delivery_street ELSE NULLIF($6, '') END,
			delivery_city        = CASE WHEN $7::text IS NULL THEN delivery_city ELSE NULLIF($7, '') END,
			delivery_postcode    = CASE WHEN $8::text IS NULL THEN delivery_postcode ELSE NULLIF($8, '') END,
			updated_at           = NOW()
		WHERE id = $1 AND deleted_at IS NULL`,
		orderID, shared.TrimPtr(r.FirstName), shared.TrimPtr(r.LastName), shared.TrimPtr(r.Phone), shared.TrimPtr(r.Email),
		shared.TrimPtr(r.Address), shared.TrimPtr(r.City), shared.TrimPtr(r.Postcode))
	return err
}

// UpdateOrderGift sets the gift message, reason and flag. Nil keeps a field;
// "" clears the message or reason.
func (s *Repo) UpdateOrderGift(ctx context.Context, orderID string, message, reason *string, isGift *bool) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE orders SET
			gift_message = CASE WHEN $2::text IS NULL THEN gift_message ELSE NULLIF($2, '') END,
			gift_reason  = CASE WHEN $3::text IS NULL THEN gift_reason ELSE NULLIF($3, '') END,
			is_gift      = COALESCE($4::boolean, is_gift),
			updated_at   = NOW()
		WHERE id = $1 AND deleted_at IS NULL`,
		orderID, shared.TrimPtr(message), shared.TrimPtr(reason), isGift)
	return err
}
