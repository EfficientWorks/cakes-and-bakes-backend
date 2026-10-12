package recipient

import (
	"context"
	"encoding/json"

	"bakehouse/services/identity/identityapi"
	"bakehouse/services/identity/internal/store/shared"
)

func (s *Repo) ListCustomerRecipients(ctx context.Context, customerID string) ([]identityapi.CustomerRecipient, error) {
	const q = `
		SELECT id, customer_id, first_name, last_name, phone, relationship, occasion_dates, created_at, updated_at
		FROM customer_recipients WHERE customer_id = $1 ORDER BY created_at DESC`
	rows, err := s.pool.Query(ctx, q, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []identityapi.CustomerRecipient
	for rows.Next() {
		var r identityapi.CustomerRecipient
		var raw []byte
		if err := rows.Scan(&r.ID, &r.CustomerID, &r.FirstName, &r.LastName, &r.Phone, &r.Relationship, &raw, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &r.OccasionDates)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Repo) CreateCustomerRecipient(ctx context.Context, r identityapi.CustomerRecipient) (*identityapi.CustomerRecipient, error) {
	occasionJSON, err := json.Marshal(r.OccasionDates)
	if err != nil {
		return nil, err
	}
	const q = `
		INSERT INTO customer_recipients (customer_id, first_name, last_name, phone, relationship, occasion_dates)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING id, customer_id, first_name, last_name, phone, relationship, occasion_dates, created_at, updated_at`
	row := s.pool.QueryRow(ctx, q, r.CustomerID, r.FirstName, r.LastName, r.Phone, r.Relationship, occasionJSON)
	var out identityapi.CustomerRecipient
	var raw []byte
	if err := row.Scan(&out.ID, &out.CustomerID, &out.FirstName, &out.LastName, &out.Phone, &out.Relationship, &raw, &out.CreatedAt, &out.UpdatedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(raw, &out.OccasionDates)
	return &out, nil
}

func (s *Repo) UpdateCustomerRecipient(ctx context.Context, customerID, recipientID string, r identityapi.CustomerRecipient) error {
	occasionJSON, err := json.Marshal(r.OccasionDates)
	if err != nil {
		return err
	}
	const q = `
		UPDATE customer_recipients SET first_name=$3, last_name=$4, phone=$5, relationship=$6, occasion_dates=$7, updated_at=NOW()
		WHERE id = $1 AND customer_id = $2`
	tag, err := s.pool.Exec(ctx, q, recipientID, customerID, r.FirstName, r.LastName, r.Phone, r.Relationship, occasionJSON)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}

func (s *Repo) DeleteCustomerRecipient(ctx context.Context, customerID, recipientID string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM customer_recipients WHERE id = $1 AND customer_id = $2`, recipientID, customerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}
