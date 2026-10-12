package recipient

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/orders/ordersapi"
)

// CreateStorefrontRecipient inserts or refreshes a recipient and returns the
// stored row. Saving the same recipient email twice updates rather than
// errors, which is what a "save this address" button on a checkout wants.
func (s *Repo) CreateStorefrontRecipient(ctx context.Context, r ordersapi.StorefrontRecipient) (*ordersapi.StorefrontRecipient, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err := UpsertStorefrontRecipient(ctx, tx, r); err != nil {
		return nil, err
	}
	q := `SELECT ` + storefrontRecipientColumns + `
		FROM storefront_recipients WHERE purchaser_email = $1 AND recipient_email = $2`
	saved, err := scanStorefrontRecipient(tx.QueryRow(ctx, q, normaliseEmail(r.PurchaserEmail), normaliseEmail(r.RecipientEmail)))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &saved, nil
}

// UpdateStorefrontRecipient replaces every editable field on one row, found
// by id alone. Unlike the upsert this is a full write — a PUT means "make it
// look like this", so a nil address part clears it. The purchaser email the
// row was saved under is kept; a signed-in customer's id is stamped on if
// the row had none.
func (s *Repo) UpdateStorefrontRecipient(ctx context.Context, id string, customerID *string, r ordersapi.StorefrontRecipient) (*ordersapi.StorefrontRecipient, error) {
	q := `
		UPDATE storefront_recipients SET
			recipient_email = $3, first_name = $4, last_name = $5, phone = $6,
			business_name = $7, delivery_address_type = $8,
			delivery_instructions = $9, delivery_postcode = $10, delivery_state = $11,
			delivery_city = $12, delivery_street = $13,
			customer_id = COALESCE(customer_id, $2::uuid),
			updated_at = NOW()
		WHERE id = $1::uuid
		RETURNING ` + storefrontRecipientColumns
	saved, err := scanStorefrontRecipient(s.pool.QueryRow(ctx, q, id, customerID,
		normaliseEmail(r.RecipientEmail), r.FirstName, r.LastName, r.Phone,
		r.BusinessName, r.DeliveryAddressType,
		r.DeliveryInstructions, r.DeliveryPostcode, r.DeliveryState, r.DeliveryCity, r.DeliveryStreet))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ordersapi.ErrNotFound
		}
		return nil, err
	}
	return &saved, nil
}

// DeleteStorefrontRecipient removes one row by id.
func (s *Repo) DeleteStorefrontRecipient(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM storefront_recipients WHERE id = $1::uuid`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ordersapi.ErrNotFound
	}
	return nil
}

// UpsertStorefrontRecipient is the write behind both a placed order and the
// explicit POST. The recipient book is keyed by (purchaser_email,
// recipient_email); an order for someone already in the book refreshes the
// name and phone but only overwrites address parts that were actually
// supplied, so a pickup order does not blank out a saved delivery address.
// customer_id is only ever set, never cleared, so a guest who later signs in
// keeps the link.
func UpsertStorefrontRecipient(ctx context.Context, tx pgx.Tx, r ordersapi.StorefrontRecipient) error {
	const q = `
		INSERT INTO storefront_recipients (
			purchaser_email, customer_id, recipient_email, first_name, last_name, phone,
			business_name, delivery_address_type,
			delivery_instructions, delivery_postcode, delivery_state, delivery_city, delivery_street)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (purchaser_email, recipient_email) DO UPDATE SET
			customer_id = COALESCE(EXCLUDED.customer_id, storefront_recipients.customer_id),
			first_name = EXCLUDED.first_name,
			last_name = EXCLUDED.last_name,
			phone = EXCLUDED.phone,
			business_name = COALESCE(NULLIF(EXCLUDED.business_name, ''), storefront_recipients.business_name),
			delivery_address_type = COALESCE(NULLIF(EXCLUDED.delivery_address_type, ''), storefront_recipients.delivery_address_type),
			delivery_instructions = COALESCE(NULLIF(EXCLUDED.delivery_instructions, ''), storefront_recipients.delivery_instructions),
			delivery_postcode = COALESCE(NULLIF(EXCLUDED.delivery_postcode, ''), storefront_recipients.delivery_postcode),
			delivery_state = COALESCE(NULLIF(EXCLUDED.delivery_state, ''), storefront_recipients.delivery_state),
			delivery_city = COALESCE(NULLIF(EXCLUDED.delivery_city, ''), storefront_recipients.delivery_city),
			delivery_street = COALESCE(NULLIF(EXCLUDED.delivery_street, ''), storefront_recipients.delivery_street),
			updated_at = NOW()`
	_, err := tx.Exec(ctx, q, normaliseEmail(r.PurchaserEmail), r.CustomerID, normaliseEmail(r.RecipientEmail),
		r.FirstName, r.LastName, r.Phone,
		r.BusinessName, r.DeliveryAddressType,
		r.DeliveryInstructions, r.DeliveryPostcode, r.DeliveryState, r.DeliveryCity, r.DeliveryStreet)
	return err
}
