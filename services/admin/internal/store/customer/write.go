package customer

import (
	"context"

	"bakehouse/services/admin/adminapi"
)

func (s *Repo) CreateCustomerNote(ctx context.Context, customerID, authorStaffID, note string) (*adminapi.CustomerNote, error) {
	const q = `
		INSERT INTO customer_notes (customer_id, author_staff_id, note)
		VALUES ($1,$2,$3)
		RETURNING id, customer_id, author_staff_id, note, created_at`
	var n adminapi.CustomerNote
	err := s.pool.QueryRow(ctx, q, customerID, authorStaffID, note).Scan(&n.ID, &n.CustomerID, &n.AuthorStaffID, &n.Note, &n.CreatedAt)
	return &n, err
}

func (s *Repo) AddCustomerTag(ctx context.Context, customerID, tag, createdBy string) error {
	const q = `INSERT INTO customer_tags (customer_id, tag, created_by) VALUES ($1,$2,$3) ON CONFLICT (customer_id, tag) DO NOTHING`
	_, err := s.pool.Exec(ctx, q, customerID, tag, createdBy)
	return err
}

func (s *Repo) RemoveCustomerTag(ctx context.Context, customerID, tag string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM customer_tags WHERE customer_id = $1 AND tag = $2`, customerID, tag)
	return err
}
