package customer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"bakehouse/pkg/tenant"
	"bakehouse/services/identity/identityapi"
	"bakehouse/services/identity/internal/store/shared"
)

// CreatePOSCustomer inserts a new POS customer affiliated with a specific
// branch, in the caller's business.
func (s *Repo) CreatePOSCustomer(ctx context.Context, branchID, firstName, lastName string, email, mobileNumber *string) (*identityapi.Customer, error) {
	const q = `
		INSERT INTO customers (
			branch_id, customer_type, first_name, last_name, email, mobile_number,
			password_hash, is_verified, created_at, updated_at, business_id
		)
		VALUES ($1, 'pos', $2, $3, $4, $5, '', true, NOW(), NOW(), $6)
		RETURNING id, branch_id, customer_type, COALESCE(email, ''), password_hash, first_name, last_name, mobile_number,
		          is_verified, marketing_opt_in, marketing_opt_in_at, date_of_birth, anniversary_date, created_at, updated_at`

	var emailVal any = nil
	if email != nil && strings.TrimSpace(*email) != "" {
		emailVal = strings.TrimSpace(*email)
	}

	row := s.pool.QueryRow(ctx, q, branchID, firstName, lastName, emailVal, mobileNumber, tenant.BusinessParam(ctx))
	return scanCustomer(row)
}

// ListPOSCustomers queries POS customers for the given branch with optional search and pagination.
func (s *Repo) ListPOSCustomers(ctx context.Context, branchID, search string, page, limit int) ([]identityapi.Customer, int, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 25
	}

	where := []string{"c.branch_id = $1", "c.customer_type = 'pos'", "c.deleted_at IS NULL"}
	args := []any{branchID}
	if tenant.Scoped(ctx) {
		args = append(args, tenant.BusinessParam(ctx))
		where = append(where, fmt.Sprintf("c.business_id IS NOT DISTINCT FROM $%d", len(args)))
	}

	if s := strings.TrimSpace(search); s != "" {
		args = append(args, "%"+s+"%")
		idx := len(args)
		where = append(where, fmt.Sprintf(
			"(c.first_name ILIKE $%d OR c.last_name ILIKE $%d OR (c.email IS NOT NULL AND c.email ILIKE $%d) OR (c.mobile_number IS NOT NULL AND c.mobile_number ILIKE $%d))",
			idx, idx, idx, idx,
		))
	}

	whereClause := strings.Join(where, " AND ")

	countQuery := "SELECT COUNT(*) FROM customers c WHERE " + whereClause
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count pos customers: %w", err)
	}

	args = append(args, limit, (page-1)*limit)
	selectQuery := fmt.Sprintf(`
		SELECT c.id, c.branch_id, c.customer_type, COALESCE(c.email, ''), c.password_hash, c.first_name, c.last_name, c.mobile_number,
		       c.is_verified, c.marketing_opt_in, c.marketing_opt_in_at, c.date_of_birth, c.anniversary_date, c.created_at, c.updated_at,
		       COALESCE(NULLIF(a.address, ''), NULLIF(a.street, '')), NULLIF(a.street, ''), NULLIF(a.city, ''),
		       NULLIF(a.state, ''), NULLIF(a.postcode, '')
		FROM customers c
		LEFT JOIN LATERAL (
			SELECT address, street, city, state, postcode FROM customer_addresses
			 WHERE customer_id = c.id ORDER BY is_default DESC, created_at DESC LIMIT 1
		) a ON true
		WHERE %s
		ORDER BY c.created_at DESC
		LIMIT $%d OFFSET $%d`, whereClause, len(args)-1, len(args))

	rows, err := s.pool.Query(ctx, selectQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list pos customers: %w", err)
	}
	defer rows.Close()

	var customers []identityapi.Customer
	for rows.Next() {
		var c identityapi.Customer
		if err := rows.Scan(
			&c.ID, &c.BranchID, &c.CustomerType, &c.Email, &c.PasswordHash, &c.FirstName, &c.LastName, &c.MobileNumber,
			&c.IsVerified, &c.MarketingOptIn, &c.MarketingOptInAt, &c.DateOfBirth, &c.AnniversaryDate, &c.CreatedAt, &c.UpdatedAt,
			&c.Address, &c.Street, &c.City, &c.State, &c.Postcode,
		); err != nil {
			return nil, 0, fmt.Errorf("scan pos customer: %w", err)
		}
		customers = append(customers, c)
	}
	if customers == nil {
		customers = []identityapi.Customer{}
	}

	return customers, total, rows.Err()
}

// GetPOSCustomerByID retrieves a POS customer by ID.
func (s *Repo) GetPOSCustomerByID(ctx context.Context, customerID string) (*identityapi.Customer, error) {
	cond, args := shared.BusinessCond(ctx, "business_id", []any{customerID})
	q := `
		SELECT id, branch_id, customer_type, COALESCE(email, ''), password_hash, first_name, last_name, mobile_number,
		       is_verified, marketing_opt_in, marketing_opt_in_at, date_of_birth, anniversary_date, created_at, updated_at
		FROM customers
		WHERE id = $1 AND customer_type = 'pos' AND deleted_at IS NULL` + cond

	row := s.pool.QueryRow(ctx, q, args...)
	return scanCustomer(row)
}

// GetPOSCustomerByEmail finds one store's POS customer by email; the email
// is unique per store, as the mobile is.
func (s *Repo) GetPOSCustomerByEmail(ctx context.Context, email, branchID string) (*identityapi.Customer, error) {
	cond, args := shared.BusinessCond(ctx, "business_id", []any{email, branchID})
	q := `
		SELECT id, branch_id, customer_type, email, password_hash, first_name, last_name, mobile_number,
		       is_verified, marketing_opt_in, marketing_opt_in_at, date_of_birth, anniversary_date, created_at, updated_at
		FROM customers
		WHERE lower(email) = lower($1) AND deleted_at IS NULL
		  AND customer_type = 'pos' AND branch_id = NULLIF($2, '')::uuid` + cond + `
		LIMIT 1`
	row := s.pool.QueryRow(ctx, q, args...)
	return scanCustomer(row)
}

// UpdatePOSCustomer writes a POS customer's details; email is written as
// given (nil clears it). shared.ErrEmailTaken when another customer has that email.
func (s *Repo) UpdatePOSCustomer(ctx context.Context, id, firstName, lastName string, email, mobile *string, dateOfBirth, anniversaryDate *time.Time) (*identityapi.Customer, error) {
	cond, args := shared.BusinessCond(ctx, "business_id", []any{id, firstName, lastName, mobile, dateOfBirth, anniversaryDate, email})
	q := `
		UPDATE customers
		SET first_name = $2, last_name = $3, mobile_number = $4, date_of_birth = $5, anniversary_date = $6,
		    email = $7, updated_at = NOW()
		WHERE id = $1 AND customer_type = 'pos' AND deleted_at IS NULL` + cond + `
		RETURNING id, branch_id, customer_type, COALESCE(email, ''), password_hash, first_name, last_name, mobile_number,
		          is_verified, marketing_opt_in, marketing_opt_in_at, date_of_birth, anniversary_date, created_at, updated_at`
	row := s.pool.QueryRow(ctx, q, args...)
	c, err := scanCustomer(row)
	if err != nil {
		if errors.Is(err, shared.ErrNotFound) {
			return nil, shared.ErrNotFound
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" &&
			(pgErr.ConstraintName == "idx_customers_email" || pgErr.ConstraintName == "idx_customers_pos_email_per_branch") {
			return nil, shared.ErrEmailTaken
		}
		return nil, fmt.Errorf("update pos customer: %w", err)
	}
	return c, nil
}

func (s *Repo) UpdatePOSCustomerAddress(
	ctx context.Context,
	customerID string,
	postCode, city, address, street, state *string,
) error {
	// Both columns: the reads prefer `address` and only fall back to
	// `street`, so writing street alone left the old address on screen.
	// If the customer has no address row yet (or none marked default),
	// one is created rather than the update silently touching nothing.
	if err := shared.CustomerInBusiness(ctx, s.pool, customerID); err != nil {
		return err
	}
	const q = `
		UPDATE customer_addresses
		SET postcode   = COALESCE($2, postcode),
		    city       = COALESCE($3, city),
		    address    = COALESCE($4, address),
		    street     = COALESCE($5, $4, street),
		    state      = COALESCE($6, state),
		    updated_at = NOW()
		WHERE id = (
			SELECT id FROM customer_addresses
			 WHERE customer_id = $1
			 ORDER BY is_default DESC, created_at DESC
			 LIMIT 1
		)`

	tag, err := s.pool.Exec(ctx, q, customerID, postCode, city, address, street, state)
	if err != nil {
		return fmt.Errorf("update pos customer address: %w", err)
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	if postCode == nil && city == nil && address == nil && street == nil && state == nil {
		return nil
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO customer_addresses (customer_id, label, address, street, city, state, postcode, country, is_default)
		VALUES ($1, 'Default', COALESCE($4, ''), COALESCE($5, $4, ''), COALESCE($3, ''), COALESCE($6, ''), COALESCE($2, ''), 'US', true)`,
		customerID, postCode, city, address, street, state)
	if err != nil {
		return fmt.Errorf("create pos customer address: %w", err)
	}
	return nil
}

// DeletePOSCustomerByID soft-deletes a POS customer by setting deleted_at.
func (s *Repo) DeletePOSCustomerByID(ctx context.Context, customerID string) error {
	cond, args := shared.BusinessCond(ctx, "business_id", []any{customerID})
	q := `
		UPDATE customers
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND customer_type = 'pos' AND deleted_at IS NULL` + cond

	tag, err := s.pool.Exec(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("delete pos customer: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return shared.ErrNotFound
	}

	return nil
}
