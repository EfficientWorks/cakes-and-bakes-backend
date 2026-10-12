package address

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/identity/identityapi"
	"bakehouse/services/identity/internal/store/shared"
)

func (s *Repo) ListCustomerAddresses(ctx context.Context, customerID string) ([]identityapi.CustomerAddress, error) {
	const q = `
		SELECT id, customer_id, label, COALESCE(address, ''), COALESCE(street, ''), COALESCE(city, ''), COALESCE(state, ''), COALESCE(postcode, ''), COALESCE(country, 'US'), is_default, created_at, updated_at
		FROM customer_addresses WHERE customer_id = $1 ORDER BY is_default DESC, created_at DESC`
	rows, err := s.pool.Query(ctx, q, customerID)
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
		RETURNING id, customer_id, label, COALESCE(address, ''), COALESCE(street, ''), COALESCE(city, ''), COALESCE(state, ''), COALESCE(postcode, ''), COALESCE(country, 'US'), is_default, created_at, updated_at`
	row := s.pool.QueryRow(ctx, q, a.CustomerID, a.Label, addr, street, a.City, a.State, a.Postcode, a.Country, a.IsDefault)
	var out identityapi.CustomerAddress
	err := row.Scan(&out.ID, &out.CustomerID, &out.Label, &out.Address, &out.Street, &out.City, &out.State, &out.Postcode, &out.Country, &out.IsDefault, &out.CreatedAt, &out.UpdatedAt)
	return &out, err
}

func (s *Repo) GetCustomerAddress(ctx context.Context, customerID, addressID string) (*identityapi.CustomerAddress, error) {
	const q = `
		SELECT id, customer_id, label, COALESCE(address, ''), COALESCE(street, ''), COALESCE(city, ''), COALESCE(state, ''), COALESCE(postcode, ''), COALESCE(country, 'US'), is_default, created_at, updated_at
		FROM customer_addresses WHERE id = $1 AND customer_id = $2`
	row := s.pool.QueryRow(ctx, q, addressID, customerID)
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
	const q = `
		UPDATE customer_addresses SET label=$3, address=$4, street=$5, city=$6, state=$7, postcode=$8, country=$9, updated_at=NOW()
		WHERE id = $1 AND customer_id = $2`
	tag, err := s.pool.Exec(ctx, q, addressID, customerID, a.Label, addr, street, a.City, a.State, a.Postcode, a.Country)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}

func (s *Repo) DeleteCustomerAddress(ctx context.Context, customerID, addressID string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM customer_addresses WHERE id = $1 AND customer_id = $2`, addressID, customerID)
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
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM customer_addresses WHERE customer_id = $1`, customerID).Scan(&n)
	return n, err
}
