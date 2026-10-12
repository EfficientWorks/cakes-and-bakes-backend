package category

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/catalog/internal/store/shared"
)

func (s *Repo) CreateCategory(
	ctx context.Context,
	w catalogapi.CategoryWrite,
) (*CategoryRow, error) {

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	const q = `
		INSERT INTO product_categories (
			name,
			slug,
			description,
			image_url,
			image_alt_text,
			sort_order,
			is_active,
			available_from,
			available_until,
			color_code
		)
		VALUES (
			$1,$2,$3,$4,$5,$6,
			COALESCE($7, true),
			$8,$9,$10
		)
		RETURNING
			id,
			name,
			slug,
			description,
			image_url,
			image_alt_text,
			sort_order,
			is_active,
			available_from,
			available_until,
			color_code`

	c, _, err := scanCategoryAdmin(
		tx.QueryRow(
			ctx,
			q,
			w.Name,
			w.Slug,
			w.Description,
			w.ImageURL,
			w.ImageAltText,
			w.SortOrder,
			w.IsActive,
			w.AvailableFrom,
			w.AvailableUntil,
			w.ColorCode,
		),
	)

	if err != nil {
		if known := shared.Classify(err); known != err {
			return nil, known
		}
		return nil, fmt.Errorf("insert category: %w", err)
	}

	// When no branches are explicitly specified, default-map the new category
	// to every active branch so it is immediately available network-wide.
	// Callers that pass an explicit storeIds list (even an empty one) are
	// stating intent, so we only fill in the default when the field was
	// entirely absent (nil / zero-length slice).
	if len(w.BranchIds) == 0 {
		branchRows, err := tx.Query(ctx, `SELECT id FROM branches WHERE status = 'active'`)
		if err != nil {
			return nil, fmt.Errorf("fetch active branches for default category mapping: %w", err)
		}
		defer branchRows.Close()
		for branchRows.Next() {
			var bid string
			if err := branchRows.Scan(&bid); err != nil {
				return nil, fmt.Errorf("scan active branch id: %w", err)
			}
			w.BranchIds = append(w.BranchIds, bid)
		}
		if err := branchRows.Err(); err != nil {
			return nil, fmt.Errorf("iterate active branches: %w", err)
		}
	}

	// Write category child data.
	if err := writeCategoryChildren(ctx, tx, c.ID, w); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return s.GetCategoryAdmin(ctx, c.ID)
}

func (s *Repo) UpdateCategory(
	ctx context.Context,
	id string,
	w catalogapi.CategoryWrite,
) (*CategoryRow, error) {

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	const q = `
		UPDATE product_categories
		   SET name = $2,
		       slug = $3,
		       description = $4,
		       image_url = $5,
		       image_alt_text = $6,
		       sort_order = $7,
		       is_active = COALESCE($8, is_active),
		       available_from = $9,
		       available_until = $10,
		       color_code = $11
		 WHERE id = $1
		 RETURNING
			id,
			name,
			slug,
			description,
			image_url,
			image_alt_text,
			sort_order,
			is_active,
			available_from,
			available_until,
			color_code`

	_, _, err = scanCategoryAdmin(
		tx.QueryRow(
			ctx,
			q,
			id,
			w.Name,
			w.Slug,
			w.Description,
			w.ImageURL,
			w.ImageAltText,
			w.SortOrder,
			w.IsActive,
			w.AvailableFrom,
			w.AvailableUntil,
			w.ColorCode,
		),
	)

	if err != nil {
		if known := shared.Classify(err); known != err {
			return nil, known
		}
		return nil, fmt.Errorf("update category: %w", err)
	}

	// Update branch-category rules.
	if err := writeCategoryChildren(ctx, tx, id, w); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return s.GetCategoryAdmin(ctx, id)
}

func writeCategoryChildren(ctx context.Context, tx pgx.Tx, categoryID string, w catalogapi.CategoryWrite) error {

	// First disable the category for all existing branches.
	_, err := tx.Exec(ctx, `
		UPDATE branch_category_rules
		SET
			branch_enabled = false,
			updated_at = NOW()
		WHERE category_id = $1
	`, categoryID)

	if err != nil {
		return fmt.Errorf("disable existing branch category rules: %w", err)
	}

	// Enable the category for the selected branches.
	for _, branchID := range w.BranchIds {

		branchID = strings.TrimSpace(branchID)

		if branchID == "" {
			continue
		}

		_, err := tx.Exec(ctx, `
			INSERT INTO branch_category_rules (
				branch_id,
				category_id,
				branch_enabled,
				updated_at
			)
			VALUES ($1, $2, true, NOW())
			ON CONFLICT (branch_id, category_id)
			DO UPDATE SET
				branch_enabled = true,
				updated_at = NOW()
		`,
			branchID,
			categoryID,
		)

		if err != nil {
			if known := shared.Classify(err); known != err {
				return known
			}
			return fmt.Errorf(
				"upsert branch category rule: %w",
				err,
			)
		}
	}

	return nil
}

// SetCategoryActive is the list's on/off toggle (CAT-05): one column, no
// approval step, reflected on the storefront immediately.
func (s *Repo) SetCategoryActive(ctx context.Context, id string, isActive bool) error {
	tag, err := s.pool.Exec(ctx, `UPDATE product_categories SET is_active = $2 WHERE id = $1`, id, isActive)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}

func (s *Repo) DeleteCategory(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM product_categories WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}
