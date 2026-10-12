package kitchen

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/franchise/franchiseapi"
	"bakehouse/services/franchise/internal/store/shared"
)

// CreateCentralKitchen writes a kitchen, its store assignment and its default
// KDS settings in one transaction — a kitchen without settings would have a
// KDS whose first GET 404s.
func (s *Repo) CreateCentralKitchen(ctx context.Context, in franchiseapi.CentralKitchenInput, createdBy string) (*franchiseapi.CentralKitchen, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var id string
	err = tx.QueryRow(ctx, `
		INSERT INTO central_kitchens (name, business_id, street, city, state, postcode, country,
			phone, email, timezone, status, manager_name, manager_phone, manager_email, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,COALESCE(NULLIF($7,''),'US'),$8,$9,COALESCE(NULLIF($10,''),'America/New_York'),
			COALESCE(NULLIF($11,''),'active'),$12,$13,$14,$15)
		RETURNING id`,
		strings.TrimSpace(shared.Deref(in.Name)), strings.TrimSpace(shared.Deref(in.BusinessID)),
		strings.TrimSpace(shared.Deref(in.Street)), strings.TrimSpace(shared.Deref(in.City)), strings.TrimSpace(shared.Deref(in.State)),
		strings.TrimSpace(shared.Deref(in.Postcode)), strings.TrimSpace(shared.Deref(in.Country)),
		shared.NilIfEmpty(strings.TrimSpace(shared.Deref(in.Phone))), shared.NilIfEmpty(strings.TrimSpace(shared.Deref(in.Email))),
		strings.TrimSpace(shared.Deref(in.Timezone)), strings.TrimSpace(shared.Deref(in.Status)),
		shared.NilIfEmpty(strings.TrimSpace(shared.Deref(in.ManagerName))), shared.NilIfEmpty(strings.TrimSpace(shared.Deref(in.ManagerPhone))),
		shared.NilIfEmpty(strings.TrimSpace(shared.Deref(in.ManagerEmail))), shared.NilIfEmpty(createdBy)).Scan(&id)
	if err != nil {
		return nil, err
	}
	if in.BranchIDs != nil {
		if err := replaceKitchenBranches(ctx, tx, id, *in.BranchIDs); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO kds_settings (scope_type, central_kitchen_id) VALUES ('central_kitchen', $1)
		ON CONFLICT DO NOTHING`, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetCentralKitchen(ctx, id)
}

// UpdateCentralKitchen changes only the fields sent.
func (s *Repo) UpdateCentralKitchen(ctx context.Context, id string, in franchiseapi.CentralKitchenInput) (*franchiseapi.CentralKitchen, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		UPDATE central_kitchens SET
			name          = COALESCE($2, name),
			business_id   = COALESCE($3, business_id),
			street        = COALESCE($4, street),
			city          = COALESCE($5, city),
			state         = COALESCE($6, state),
			postcode      = COALESCE($7, postcode),
			country       = COALESCE($8, country),
			phone         = CASE WHEN $9::text IS NULL THEN phone ELSE NULLIF($9, '') END,
			email         = CASE WHEN $10::text IS NULL THEN email ELSE NULLIF($10, '') END,
			timezone      = COALESCE($11, timezone),
			status        = COALESCE($12, status),
			manager_name  = CASE WHEN $13::text IS NULL THEN manager_name ELSE NULLIF($13, '') END,
			manager_phone = CASE WHEN $14::text IS NULL THEN manager_phone ELSE NULLIF($14, '') END,
			manager_email = CASE WHEN $15::text IS NULL THEN manager_email ELSE NULLIF($15, '') END,
			updated_at    = NOW()
		 WHERE id = $1 AND deleted_at IS NULL`,
		id, shared.TrimmedPtr(in.Name), shared.TrimmedPtr(in.BusinessID), shared.TrimmedPtr(in.Street), shared.TrimmedPtr(in.City),
		shared.TrimmedPtr(in.State), shared.TrimmedPtr(in.Postcode), shared.TrimmedPtr(in.Country), shared.TrimmedPtr(in.Phone),
		shared.TrimmedPtr(in.Email), shared.TrimmedPtr(in.Timezone), shared.TrimmedPtr(in.Status),
		shared.TrimmedPtr(in.ManagerName), shared.TrimmedPtr(in.ManagerPhone), shared.TrimmedPtr(in.ManagerEmail))
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, shared.ErrNotFound
	}
	if in.BranchIDs != nil {
		if err := replaceKitchenBranches(ctx, tx, id, *in.BranchIDs); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetCentralKitchen(ctx, id)
}

// DeleteCentralKitchen soft-deletes a kitchen. Its staff assignments stay in
// the table for history, and sign-in refuses them because the kitchen is gone.
func (s *Repo) DeleteCentralKitchen(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE central_kitchens SET deleted_at = NOW(), status = 'inactive', updated_at = NOW()
		 WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}

func replaceKitchenBranches(ctx context.Context, tx pgx.Tx, kitchenID string, branchIDs []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM central_kitchen_branches WHERE central_kitchen_id = $1`, kitchenID); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, b := range branchIDs {
		b = strings.TrimSpace(b)
		if b == "" || seen[b] {
			continue
		}
		seen[b] = true
		if _, err := tx.Exec(ctx, `
			INSERT INTO central_kitchen_branches (central_kitchen_id, branch_id) VALUES ($1, $2)`, kitchenID, b); err != nil {
			return err
		}
	}
	return nil
}
