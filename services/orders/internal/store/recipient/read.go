package recipient

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/orders/ordersapi"
)

// ListStorefrontRecipients returns the book for a purchaser. When customerID
// is non-nil the rows saved under that account are included too, so a
// customer who ordered as a guest before signing up still sees them.
func (s *Repo) ListStorefrontRecipients(ctx context.Context, purchaserEmail string, customerID *string) ([]ordersapi.StorefrontRecipient, error) {
	q := `SELECT ` + storefrontRecipientColumns + `
		FROM storefront_recipients
		WHERE purchaser_email = $1 OR ($2::uuid IS NOT NULL AND customer_id = $2::uuid)
		ORDER BY updated_at DESC, created_at DESC`
	rows, err := s.pool.Query(ctx, q, normaliseEmail(purchaserEmail), customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]ordersapi.StorefrontRecipient, 0)
	for rows.Next() {
		recipient, err := scanStorefrontRecipient(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, recipient)
	}
	return result, rows.Err()
}

// GetStorefrontRecipient fetches one row by id.
func (s *Repo) GetStorefrontRecipient(ctx context.Context, id string) (*ordersapi.StorefrontRecipient, error) {
	q := `SELECT ` + storefrontRecipientColumns + `
		FROM storefront_recipients
		WHERE id = $1::uuid`
	r, err := scanStorefrontRecipient(s.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ordersapi.ErrNotFound
		}
		return nil, err
	}
	return &r, nil
}
