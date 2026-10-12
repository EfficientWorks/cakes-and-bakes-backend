package customer

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"bakehouse/pkg/tenant"
	"bakehouse/services/admin/adminapi"
)

// customerInBusiness is the condition that customer $1 belongs to the
// caller's business. $n is tenant.Scoped and $n+1 tenant.BusinessParam, so
// system work (not scoped) is never filtered.
func customerInBusiness(n int) string {
	return fmt.Sprintf(`(NOT $%d::boolean OR EXISTS (
		SELECT 1 FROM customers cb WHERE cb.id = $1::uuid AND cb.business_id IS NOT DISTINCT FROM $%d::uuid))`, n, n+1)
}

// CreateCustomerNote writes nothing and returns pgx.ErrNoRows when the
// customer belongs to another business.
func (s *Repo) CreateCustomerNote(ctx context.Context, customerID, authorStaffID, note string) (*adminapi.CustomerNote, error) {
	q := `
		INSERT INTO customer_notes (customer_id, author_staff_id, note)
		SELECT $1::uuid, $4::uuid, $5 WHERE ` + customerInBusiness(2) + `
		RETURNING id, customer_id, author_staff_id, note, created_at`
	var n adminapi.CustomerNote
	err := s.pool.QueryRow(ctx, q, customerID, tenant.Scoped(ctx), tenant.BusinessParam(ctx), authorStaffID, note).
		Scan(&n.ID, &n.CustomerID, &n.AuthorStaffID, &n.Note, &n.CreatedAt)
	return &n, err
}

// AddCustomerTag returns pgx.ErrNoRows for another business's customer.
// Ownership is checked first rather than read off the rows touched, because
// re-adding an existing tag also touches none and is not an error.
func (s *Repo) AddCustomerTag(ctx context.Context, customerID, tag, createdBy string) error {
	var owned bool
	if err := s.pool.QueryRow(ctx, `SELECT `+customerInBusiness(2),
		customerID, tenant.Scoped(ctx), tenant.BusinessParam(ctx)).Scan(&owned); err != nil {
		return err
	}
	if !owned {
		return pgx.ErrNoRows
	}
	const q = `INSERT INTO customer_tags (customer_id, tag, created_by) VALUES ($1,$2,$3) ON CONFLICT (customer_id, tag) DO NOTHING`
	_, err := s.pool.Exec(ctx, q, customerID, tag, createdBy)
	return err
}

// RemoveCustomerTag removes nothing from another business's customer.
func (s *Repo) RemoveCustomerTag(ctx context.Context, customerID, tag string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM customer_tags WHERE customer_id = $1 AND tag = $4 AND `+customerInBusiness(2),
		customerID, tenant.Scoped(ctx), tenant.BusinessParam(ctx), tag)
	return err
}
