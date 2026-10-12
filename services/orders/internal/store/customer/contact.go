package customer

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"bakehouse/services/orders/internal/store/shared"
)

// GetCustomerContact reads a live customer's name and contact details.
func (s *Repo) GetCustomerContact(ctx context.Context, customerID string) (*CustomerContact, error) {
	var c CustomerContact
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(first_name, ''), COALESCE(last_name, ''), COALESCE(email, ''), mobile_number
		  FROM customers WHERE id::text = $1 AND deleted_at IS NULL`, strings.TrimSpace(customerID)).
		Scan(&c.FirstName, &c.LastName, &c.Email, &c.Phone)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCustomerNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// UpdateCustomerContact writes name, email and mobile onto a customer record.
// A nil or blank field keeps what the record has — a till that only knows the
// phone number does not blank the name.
func (s *Repo) UpdateCustomerContact(ctx context.Context, customerID string, firstName, lastName, email, phone *string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE customers SET
			first_name    = COALESCE($2, first_name),
			last_name     = COALESCE($3, last_name),
			email         = COALESCE($4, email),
			mobile_number = COALESCE($5, mobile_number),
			updated_at    = NOW()
		WHERE id::text = $1 AND deleted_at IS NULL`,
		strings.TrimSpace(customerID), shared.NilIfEmptyPtr(firstName), shared.NilIfEmptyPtr(lastName),
		shared.NilIfEmptyPtr(email), shared.NilIfEmptyPtr(phone))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrCustomerContactTaken
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrCustomerNotFound
	}
	return nil
}

func (s *Repo) CustomerExists(ctx context.Context, id, field string) error {
	e := &CustomerExistsError{ID: id, Field: field}
	_ = s.pool.QueryRow(ctx,
		`SELECT btrim(COALESCE(first_name, '') || ' ' || COALESCE(last_name, '')) FROM customers WHERE id = $1`, id).Scan(&e.Name)
	return e
}

// IsEmailTaken: the write failed because another customer has that email
// (unique per store among POS customers, and among website accounts).
func IsEmailTaken(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" &&
		(pgErr.ConstraintName == "idx_customers_email" || pgErr.ConstraintName == "idx_customers_pos_email_per_branch")
}
