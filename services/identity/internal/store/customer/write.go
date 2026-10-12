package customer

import (
	"context"
	"fmt"

	"bakehouse/pkg/tenant"
	"bakehouse/services/identity/identityapi"
	"bakehouse/services/identity/internal/store/shared"
)

// CreateCustomer opens a website account in the caller's business.
func (s *Repo) CreateCustomer(ctx context.Context, email, passwordHash string) (*identityapi.Customer, error) {
	const q = `
		INSERT INTO customers (business_id, email, password_hash, first_name, last_name, customer_type)
		VALUES ($1, $2, $3, '', '', 'web')
		RETURNING id, branch_id, customer_type, email, password_hash, first_name, last_name, mobile_number,
		          is_verified, marketing_opt_in, marketing_opt_in_at, date_of_birth, anniversary_date, created_at, updated_at`
	row := s.pool.QueryRow(ctx, q, tenant.BusinessParam(ctx), email, passwordHash)
	return scanCustomer(row)
}

func (s *Repo) UpdateCustomerProfile(ctx context.Context, id, firstName, lastName string, mobile *string) error {
	cond, args := shared.BusinessCond(ctx, "business_id", []any{id, firstName, lastName, mobile})
	q := `
		UPDATE customers SET first_name = $2, last_name = $3, mobile_number = $4, updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL` + cond
	tag, err := s.pool.Exec(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("update customer profile: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}

func (s *Repo) SetCustomerMarketingOptIn(ctx context.Context, customerID string, optIn bool, ip string) error {
	cond, args := shared.BusinessCond(ctx, "business_id", []any{customerID, optIn, ip})
	q := `UPDATE customers SET marketing_opt_in = $2, marketing_opt_in_at = NOW(), marketing_opt_in_ip = $3, updated_at = NOW() WHERE id = $1` + cond
	_, err := s.pool.Exec(ctx, q, args...)
	return err
}

// SoftDeleteCustomer anonymises PII (ACC-05, NFR-08) and soft-deletes the
// row; order records elsewhere reference customer_id, which is left intact
// so history stays queryable, just no longer attributable to a real person.
func (s *Repo) SoftDeleteCustomer(ctx context.Context, customerID string) error {
	cond, args := shared.BusinessCond(ctx, "business_id", []any{customerID})
	q := `
		UPDATE customers SET
			email = 'deleted_' || id || '@removed.bakedbouquet.com',
			first_name = 'Deleted', last_name = 'User', mobile_number = NULL,
			password_hash = '', deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1` + cond
	_, err := s.pool.Exec(ctx, q, args...)
	return err
}
