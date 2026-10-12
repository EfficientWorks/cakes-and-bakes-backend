package customer

import (
	"context"

	"bakehouse/services/identity/identityapi"
)

// GetCustomerByEmail finds a website account by email. POS customers are
// not accounts and are looked up per store with GetPOSCustomerByEmail.
func (s *Repo) GetCustomerByEmail(ctx context.Context, email string) (*identityapi.Customer, error) {
	const q = `
		SELECT id, branch_id, customer_type, email, password_hash, first_name, last_name, mobile_number,
		       is_verified, marketing_opt_in, marketing_opt_in_at, date_of_birth, anniversary_date, created_at, updated_at
		FROM customers WHERE lower(email) = lower($1) AND deleted_at IS NULL AND customer_type <> 'pos'`
	row := s.pool.QueryRow(ctx, q, email)
	return scanCustomer(row)
}

// GetCustomerByMobile finds a live customer by phone, compared on digits only
// so the caller need not care how the number was typed.
//
// The search is within one uniqueness scope: a store's POS customers when
// branchID is set (a POS customer belongs to one store and the number is
// unique per store), or the website accounts when it is empty.
func (s *Repo) GetCustomerByMobile(ctx context.Context, mobile, branchID string) (*identityapi.Customer, error) {
	const q = `
		SELECT id, branch_id, customer_type, email, password_hash, first_name, last_name, mobile_number,
		       is_verified, marketing_opt_in, marketing_opt_in_at, date_of_birth, anniversary_date, created_at, updated_at
		FROM customers
		WHERE normalise_mobile(mobile_number) = normalise_mobile($1) AND deleted_at IS NULL
		  AND CASE WHEN $2 = '' THEN customer_type <> 'pos'
		           ELSE customer_type = 'pos' AND branch_id = NULLIF($2, '')::uuid END
		LIMIT 1`
	row := s.pool.QueryRow(ctx, q, mobile, branchID)
	return scanCustomer(row)
}

func (s *Repo) GetCustomerByID(ctx context.Context, id string) (*identityapi.Customer, error) {
	const q = `
		SELECT id, branch_id, customer_type, email, password_hash, first_name, last_name, mobile_number,
		       is_verified, marketing_opt_in, marketing_opt_in_at, date_of_birth, anniversary_date, created_at, updated_at
		FROM customers WHERE id = $1 AND deleted_at IS NULL`
	row := s.pool.QueryRow(ctx, q, id)
	return scanCustomer(row)
}
