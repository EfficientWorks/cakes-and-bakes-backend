package category

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"bakehouse/pkg/tenant"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/catalog/internal/store/shared"
)

func (s *Repo) ListCategories(ctx context.Context) ([]catalogapi.Category, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, slug, description, image_url, image_alt_text, sort_order, is_active, color_code
		FROM product_categories
		 WHERE is_active = true AND business_id IS NOT DISTINCT FROM $1
		 ORDER BY sort_order`, tenant.BusinessParam(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []catalogapi.Category
	for rows.Next() {
		var c catalogapi.Category
		if err := rows.Scan(&c.ID, &c.Name, &c.Slug, &c.Description,
			&c.ImageURL, &c.ImageAltText, &c.SortOrder, &c.IsActive, &c.ColorCode); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListCategoriesForBranch is the category list as one store sees it: active
// categories the branch has enabled, that hold at least one product mapped
// to the branch. A category with nothing to sell in it is a dead tab on a
// till, so it is left out.
func (s *Repo) ListCategoriesForBranch(ctx context.Context, branchID string) ([]catalogapi.Category, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.name, c.slug, c.description, c.image_url, c.image_alt_text, c.sort_order, c.is_active, c.color_code
		  FROM product_categories c
		  JOIN branch_category_rules bcr ON bcr.category_id = c.id AND bcr.branch_id = $1 AND bcr.branch_enabled
		 WHERE c.is_active = true AND c.business_id IS NOT DISTINCT FROM $2
		   AND EXISTS (SELECT 1
		                 FROM products p
		                 JOIN branch_product_rules bpr ON bpr.product_id = p.id AND bpr.branch_id = $1 AND bpr.branch_enabled
		                WHERE p.category_id = c.id AND p.deleted_at IS NULL AND p.is_active = true)
		 ORDER BY c.sort_order`, branchID, tenant.BusinessParam(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []catalogapi.Category{}
	for rows.Next() {
		var c catalogapi.Category
		if err := rows.Scan(&c.ID, &c.Name, &c.Slug, &c.Description,
			&c.ImageURL, &c.ImageAltText, &c.SortOrder, &c.IsActive, &c.ColorCode); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Repo) GetCategoryBySlug(ctx context.Context, slug string) (*catalogapi.Category, error) {
	var c catalogapi.Category
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, slug, description, sort_order, is_active, image_url, image_alt_text, color_code
		FROM product_categories WHERE slug = $1 AND business_id IS NOT DISTINCT FROM $2`, slug, tenant.BusinessParam(ctx)).
		Scan(&c.ID, &c.Name, &c.Slug, &c.Description, &c.SortOrder, &c.IsActive, &c.ImageURL, &c.ImageAltText, &c.ColorCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.ErrNotFound
	}
	return &c, err
}

func (s *Repo) GetCategoryByID(ctx context.Context, id string) (*catalogapi.Category, error) {
	var c catalogapi.Category
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, slug, description, image_url, image_alt_text, sort_order, is_active, color_code
		FROM product_categories WHERE id = $1 AND business_id IS NOT DISTINCT FROM $2`, id, tenant.BusinessParam(ctx)).
		Scan(&c.ID, &c.Name, &c.Slug, &c.Description, &c.ImageURL, &c.ImageAltText, &c.SortOrder, &c.IsActive, &c.ColorCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.ErrNotFound
	}
	return &c, err
}

// ListCategoriesAdmin returns every category, inactive ones included — the
// admin list has a status filter and cannot filter what it was never sent.
//
// branchIDs, when given, narrows the list to categories enabled at any of
// those branches; nil means the whole master catalogue.
func (s *Repo) ListCategoriesAdmin(ctx context.Context, branchIDs []string) ([]CategoryRow, error) {
	q := categoryAdminList + ` ORDER BY pc.sort_order, pc.name`
	args := []any{tenant.BusinessParam(ctx)}
	if branchIDs != nil {
		q = categoryMappedToBranches + ` ORDER BY pc.sort_order, pc.name`
		args = append(args, branchIDs)
	}
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CategoryRow
	for rows.Next() {
		var c catalogapi.Category
		var w catalogapi.CategoryWrite
		var branchIDs []string
		if err := rows.Scan(
			&c.ID, &c.Name, &c.Slug, &c.Description, &c.ImageURL, &c.ImageAltText,
			&c.SortOrder, &c.IsActive, &w.AvailableFrom, &w.AvailableUntil, &c.ColorCode,
			&branchIDs,
		); err != nil {
			return nil, err
		}
		w.ColorCode = c.ColorCode
		out = append(out, CategoryRow{Category: c, Schedule: w, BranchIds: branchIDs})
	}
	return out, rows.Err()
}

func (s *Repo) GetCategoryAdmin(ctx context.Context, id string) (*CategoryRow, error) {
	const q = categoryAdminList + ` HAVING pc.id = $2`
	var c catalogapi.Category
	var w catalogapi.CategoryWrite
	var branchIDs []string
	err := s.pool.QueryRow(ctx, q, tenant.BusinessParam(ctx), id).Scan(
		&c.ID, &c.Name, &c.Slug, &c.Description, &c.ImageURL, &c.ImageAltText,
		&c.SortOrder, &c.IsActive, &w.AvailableFrom, &w.AvailableUntil, &c.ColorCode,
		&branchIDs,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	w.ColorCode = c.ColorCode
	return &CategoryRow{Category: c, Schedule: w, BranchIds: branchIDs}, nil
}

// CountProductsInCategory returns how many products the category holds: the
// ones customers can see, and the total including deleted ones.
//
// Both matter, and for different reasons. The live count is what a person
// needs to hear about. The total is what the foreign key actually enforces —
// a deleted product is only marked deleted, so it still points at its
// category and still blocks the row from going. Counting only the live ones
// let the delete through to the database, which refused it as a raw
// constraint violation the console could only show as "an unexpected error".
func (s *Repo) CountProductsInCategory(ctx context.Context, id string) (live int, total int, err error) {
	err = s.pool.QueryRow(ctx,
		`SELECT count(*) FILTER (WHERE deleted_at IS NULL), count(*)
		   FROM products WHERE category_id = $1 AND business_id IS NOT DISTINCT FROM $2`, id, tenant.BusinessParam(ctx)).Scan(&live, &total)
	return live, total, err
}
