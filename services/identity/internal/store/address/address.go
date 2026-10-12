package address

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/identity/identityapi"
	"bakehouse/services/identity/internal/store/shared"
)

// Every statement is limited to a customer of the caller's business
// (shared.CustomerCond), so a customer id from another business — an old
// token sent to this storefront, a till's id from elsewhere — finds nothing.

const addressCols = `id, customer_id, label, COALESCE(address, ''), COALESCE(street, ''), COALESCE(city, ''), COALESCE(state, ''), COALESCE(postcode, ''), COALESCE(country, 'US'), is_default, created_at, updated_at`

func (s *Repo) ListCustomerAddresses(ctx context.Context, customerID string) ([]identityapi.CustomerAddress, error) {
	cond, args := shared.CustomerCond(ctx, "customer_addresses.customer_id", []any{customerID})
	q := `SELECT ` + addressCols + `
		FROM customer_addresses WHERE customer_id = $1` + cond + ` ORDER BY is_default DESC, created_at DESC`
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []identityapi.CustomerAddress
	for rows.Next() {
		var a identityapi.CustomerAddress
		if err := rows.Scan(&a.ID, &a.CustomerID, &a.Label, &a.Address, &a.Street, &a.City, &a.State, &a.Postcode, &a.Country, &a.IsDefault, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Repo) CreateCustomerAddress(ctx context.Context, a identityapi.CustomerAddress) (*identityapi.CustomerAddress, error) {
	if err := shared.CustomerInBusiness(ctx, s.pool, a.CustomerID); err != nil {
		return nil, err
	}
	if a.IsDefault {
		if _, err := s.pool.Exec(ctx, `UPDATE customer_addresses SET is_default = false WHERE customer_id = $1`, a.CustomerID); err != nil {
			return nil, err
		}
	}
	addr := a.Address
	if addr == "" {
		addr = a.Street
	}
	street := a.Street
	if street == "" {
		street = a.Address
	}
	const q = `
		INSERT INTO customer_addresses (customer_id, label, address, street, city, state, postcode, country, is_default)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING ` + addressCols
	row := s.pool.QueryRow(ctx, q, a.CustomerID, a.Label, addr, street, a.City, a.State, a.Postcode, a.Country, a.IsDefault)
	var out identityapi.CustomerAddress
	err := row.Scan(&out.ID, &out.CustomerID, &out.Label, &out.Address, &out.Street, &out.City, &out.State, &out.Postcode, &out.Country, &out.IsDefault, &out.CreatedAt, &out.UpdatedAt)
	return &out, err
}

func (s *Repo) GetCustomerAddress(ctx context.Context, customerID, addressID string) (*identityapi.CustomerAddress, error) {
	cond, args := shared.CustomerCond(ctx, "customer_addresses.customer_id", []any{addressID, customerID})
	q := `SELECT ` + addressCols + `
		FROM customer_addresses WHERE id = $1 AND customer_id = $2` + cond
	row := s.pool.QueryRow(ctx, q, args...)
	var out identityapi.CustomerAddress
	err := row.Scan(&out.ID, &out.CustomerID, &out.Label, &out.Address, &out.Street, &out.City, &out.State, &out.Postcode, &out.Country, &out.IsDefault, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.ErrNotFound
	}
	return &out, err
}

func (s *Repo) UpdateCustomerAddress(ctx context.Context, customerID, addressID string, a identityapi.CustomerAddress) error {
	addr := a.Address
	if addr == "" {
		addr = a.Street
	}
	street := a.Street
	if street == "" {
		street = a.Address
	}
	cond, args := shared.CustomerCond(ctx, "customer_addresses.customer_id",
		[]any{addressID, customerID, a.Label, addr, street, a.City, a.State, a.Postcode, a.Country})
	q := `
		UPDATE customer_addresses SET label=$3, address=$4, street=$5, city=$6, state=$7, postcode=$8, country=$9, updated_at=NOW()
		WHERE id = $1 AND customer_id = $2` + cond
	tag, err := s.pool.Exec(ctx, q, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}

func (s *Repo) DeleteCustomerAddress(ctx context.Context, customerID, addressID string) error {
	cond, args := shared.CustomerCond(ctx, "customer_addresses.customer_id", []any{addressID, customerID})
	tag, err := s.pool.Exec(ctx, `DELETE FROM customer_addresses WHERE id = $1 AND customer_id = $2`+cond, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}

func (s *Repo) SetDefaultAddress(ctx context.Context, customerID, addressID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Checked first: the reset below would otherwise clear another
	// business's customer's default before the set found nothing.
	if err := shared.CustomerInBusiness(ctx, tx, customerID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE customer_addresses SET is_default = false WHERE customer_id = $1`, customerID); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE customer_addresses SET is_default = true WHERE id = $1 AND customer_id = $2`, addressID, customerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return tx.Commit(ctx)
}

func (s *Repo) CountCustomerAddresses(ctx context.Context, customerID string) (int, error) {
	cond, args := shared.CustomerCond(ctx, "customer_addresses.customer_id", []any{customerID})
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM customer_addresses WHERE customer_id = $1`+cond, args...).Scan(&n)
	return n, err
}
