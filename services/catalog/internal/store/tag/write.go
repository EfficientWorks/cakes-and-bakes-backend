package tag

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"

	"bakehouse/pkg/tenant"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/catalog/internal/store/shared"
)

// CreateTag saves a new tag. shared.ErrDuplicate when the name's slug is taken.
func (s *Repo) CreateTag(ctx context.Context, t catalogapi.SeasonalTag) (*catalogapi.SeasonalTag, error) {
	var id string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO seasonal_tags (name, slug, description, status, restricted, restricted_from, restricted_to, repeat_yearly, business_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
		t.Name, t.Slug, t.Description, t.Status, t.Restricted, t.RestrictedFrom, t.RestrictedTo, t.RepeatYearly,
		tenant.BusinessParam(ctx)).Scan(&id)
	if err != nil {
		return nil, tagWriteError(err)
	}
	return s.GetTag(ctx, id)
}

// UpdateTag writes every field of a tag, and the slugs on its products.
func (s *Repo) UpdateTag(ctx context.Context, t catalogapi.SeasonalTag) (*catalogapi.SeasonalTag, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE seasonal_tags SET name = $2, slug = $3, description = $4, status = $5, restricted = $6,
		       restricted_from = $7, restricted_to = $8, repeat_yearly = $9, updated_at = NOW()
		 WHERE id::text = $1 AND deleted_at IS NULL AND business_id IS NOT DISTINCT FROM $10`,
		t.ID, t.Name, t.Slug, t.Description, t.Status, t.Restricted, t.RestrictedFrom, t.RestrictedTo, t.RepeatYearly,
		tenant.BusinessParam(ctx))
	if err != nil {
		return nil, tagWriteError(err)
	}
	if tag.RowsAffected() == 0 {
		return nil, shared.ErrNotFound
	}
	if err := s.syncOccasionTagsForTag(ctx, t.ID); err != nil {
		return nil, err
	}
	return s.GetTag(ctx, t.ID)
}

// DeleteTag removes a tag and unmaps it from every product.
func (s *Repo) DeleteTag(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE seasonal_tags SET deleted_at = NOW()
		  WHERE id::text = $1 AND deleted_at IS NULL AND business_id IS NOT DISTINCT FROM $2`, id, tenant.BusinessParam(ctx))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	var products []string
	rows, err := s.pool.Query(ctx, `DELETE FROM product_seasonal_tags WHERE tag_id::text = $1 RETURNING product_id::text`, id)
	if err != nil {
		return err
	}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err == nil {
			products = append(products, p)
		}
	}
	rows.Close()
	for _, p := range products {
		if err := s.syncOccasionTags(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

// SetProductTags replaces a product's tags. shared.ErrRelatedMissing when an id is
// not a live tag.
func (s *Repo) SetProductTags(ctx context.Context, productID string, tagIDs []string) error {
	ids := shared.UniqueStrings(tagIDs)
	if len(ids) > 0 {
		var found int
		if err := s.pool.QueryRow(ctx, `
			SELECT count(*) FROM seasonal_tags
			 WHERE id::text = ANY($1) AND deleted_at IS NULL AND business_id IS NOT DISTINCT FROM $2`,
			ids, tenant.BusinessParam(ctx)).Scan(&found); err != nil {
			return err
		}
		if found != len(ids) {
			return shared.ErrRelatedMissing
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `DELETE FROM product_seasonal_tags WHERE product_id::text = $1`, productID); err != nil {
		return err
	}
	if len(ids) > 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO product_seasonal_tags (product_id, tag_id)
			SELECT $1::uuid, t.id FROM seasonal_tags t WHERE t.id::text = ANY($2)`, productID, ids); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return s.syncOccasionTags(ctx, productID)
}

func tagWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return shared.ErrDuplicate
	}
	return err
}
