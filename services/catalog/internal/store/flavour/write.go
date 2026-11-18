package flavour

import (
	"context"

	"github.com/jackc/pgx/v5"

	"bakehouse/pkg/tenant"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/catalog/internal/store/shared"
)

// CreateFlavour saves a new flavour and maps its products, in one
// transaction. shared.ErrDuplicate when the name's slug is taken;
// shared.ErrRelatedMissing when a product id is not one of this business's.
func (s *Repo) CreateFlavour(ctx context.Context, f catalogapi.Flavour) (*catalogapi.Flavour, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id string
	err = tx.QueryRow(ctx, `
		INSERT INTO flavours (name, slug, description, image_url, image_alt_text, background_colour, sort_order, status, business_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
		f.Name, f.Slug, f.Description, f.ImageURL, f.ImageAltText, f.BackgroundColour, f.SortOrder, f.Status,
		tenant.BusinessParam(ctx)).Scan(&id)
	if err != nil {
		return nil, shared.Classify(err)
	}
	if err := setProducts(ctx, tx, id, f.ProductIDs); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetFlavour(ctx, id)
}

// UpdateFlavour writes every field and, when ProductIDs is not nil, replaces
// the mapped products.
func (s *Repo) UpdateFlavour(ctx context.Context, f catalogapi.Flavour) (*catalogapi.Flavour, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	res, err := tx.Exec(ctx, `
		UPDATE flavours SET name = $2, slug = $3, description = $4, image_url = $5, image_alt_text = $6,
		       background_colour = $7, sort_order = $8, status = $9, updated_at = NOW()
		 WHERE id::text = $1 AND deleted_at IS NULL AND business_id IS NOT DISTINCT FROM $10`,
		f.ID, f.Name, f.Slug, f.Description, f.ImageURL, f.ImageAltText, f.BackgroundColour, f.SortOrder, f.Status,
		tenant.BusinessParam(ctx))
	if err != nil {
		return nil, shared.Classify(err)
	}
	if res.RowsAffected() == 0 {
		return nil, shared.ErrNotFound
	}
	if f.ProductIDs != nil {
		if err := setProducts(ctx, tx, f.ID, f.ProductIDs); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetFlavour(ctx, f.ID)
}

// DeleteFlavour removes a flavour and unmaps its products.
func (s *Repo) DeleteFlavour(ctx context.Context, id string) error {
	res, err := s.pool.Exec(ctx, `UPDATE flavours SET deleted_at = NOW()
		  WHERE id::text = $1 AND deleted_at IS NULL AND business_id IS NOT DISTINCT FROM $2`, id, tenant.BusinessParam(ctx))
	if err != nil {
		return shared.Classify(err)
	}
	if res.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	_, err = s.pool.Exec(ctx, `DELETE FROM product_flavours WHERE flavour_id::text = $1`, id)
	return err
}

// setProducts replaces a flavour's products. Every id must be an undeleted
// product of the request's business, or nothing is written.
func setProducts(ctx context.Context, tx pgx.Tx, flavourID string, productIDs []string) error {
	ids := shared.UniqueStrings(productIDs)
	if len(ids) > 0 {
		var found int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM products
			 WHERE id::text = ANY($1) AND deleted_at IS NULL AND business_id IS NOT DISTINCT FROM $2`,
			ids, tenant.BusinessParam(ctx)).Scan(&found); err != nil {
			return err
		}
		if found != len(ids) {
			return shared.ErrRelatedMissing
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM product_flavours WHERE flavour_id::text = $1`, flavourID); err != nil {
		return err
	}
	if len(ids) > 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO product_flavours (product_id, flavour_id)
			SELECT p.id, $1::uuid FROM products p WHERE p.id::text = ANY($2)`, flavourID, ids); err != nil {
			return err
		}
	}
	return nil
}
