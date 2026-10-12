package product

import (
	"context"
	"fmt"

	"bakehouse/pkg/tenant"
	"bakehouse/services/catalog/catalogapi"
)

func (s *Repo) GetProductByID(ctx context.Context, id string) (*catalogapi.Product, error) {
	return s.getProduct(ctx, productScoped+" AND p.id = $2", id)
}

func (s *Repo) GetProductBySlug(ctx context.Context, slug string) (*catalogapi.Product, error) {
	return s.getProduct(ctx, productScoped+" AND p.slug = $2", slug)
}

func (s *Repo) getProduct(ctx context.Context, query, arg string) (*catalogapi.Product, error) {
	p, err := scanProduct(s.pool.QueryRow(ctx, query, tenant.BusinessParam(ctx), arg))
	if err != nil {
		return nil, err
	}
	if err := s.loadChildren(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Repo) ListProducts(ctx context.Context, categoryID, occasion string, includeInactive bool) ([]catalogapi.Product, error) {
	q := productScoped
	args := []interface{}{tenant.BusinessParam(ctx)}
	if categoryID != "" {
		args = append(args, categoryID)
		q += fmt.Sprintf(" AND p.category_id = $%d", len(args))
	}
	if occasion != "" {
		args = append(args, occasion)
		q += fmt.Sprintf(" AND $%d = ANY(p.occasion_tags)", len(args))
	}
	if !includeInactive {
		q += " AND p.is_active = true" +
			// " AND (p.available_from IS NULL OR p.available_from <= CURRENT_DATE)" +
			// " AND (p.available_until IS NULL OR p.available_until >= CURRENT_DATE)" +
			" AND c.is_active = true"
		// " AND (c.available_from IS NULL OR c.available_from <= CURRENT_DATE)" +
		// " AND (c.available_until IS NULL OR c.available_until >= CURRENT_DATE)"
	}
	q += " ORDER BY p.sort_order, p.name"

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	var out []catalogapi.Product
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, *p)
	}
	rows.Close()

	for i := range out {
		if err := s.loadListChildren(ctx, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ListProductsAdminPage returns one page and the number of products the
// filters match, which is what the pager needs to know how many pages exist.
func (s *Repo) ListProductsAdminPage(ctx context.Context, q AdminProductQuery) ([]catalogapi.Product, int, error) {
	where := " AND p.business_id IS NOT DISTINCT FROM $1"
	args := []interface{}{tenant.BusinessParam(ctx)}
	if q.CategoryID != "" {
		args = append(args, q.CategoryID)
		where += fmt.Sprintf(" AND p.category_id = $%d", len(args))
	}
	if q.Search != "" {
		args = append(args, "%"+q.Search+"%")
		where += fmt.Sprintf(" AND (p.name ILIKE $%d OR p.slug ILIKE $%d)", len(args), len(args))
	}
	if q.ShowOnPos != nil {
		args = append(args, *q.ShowOnPos)
		where += fmt.Sprintf(" AND p.show_on_pos = $%d", len(args))
	}
	if q.ShowOnWebsite != nil {
		args = append(args, *q.ShowOnWebsite)
		where += fmt.Sprintf(" AND p.show_on_website = $%d", len(args))
	}
	if q.BranchIDs != nil {
		args = append(args, q.BranchIDs)
		where += fmt.Sprintf(`
			AND EXISTS (SELECT 1 FROM branch_product_rules bpr
			             JOIN branch_category_rules bcr
			               ON bcr.branch_id = bpr.branch_id AND bcr.category_id = p.category_id AND bcr.branch_enabled
			             WHERE bpr.product_id = p.id AND bpr.branch_enabled AND bpr.branch_id::text = ANY($%d))`, len(args))
	}

	var total int
	countQ := `SELECT count(*) FROM products p JOIN product_categories c ON c.id = p.category_id
	           WHERE p.deleted_at IS NULL` + where
	if err := s.pool.QueryRow(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count admin products: %w", err)
	}
	if total == 0 {
		return nil, 0, nil
	}

	if q.Limit <= 0 {
		q.Limit = 25
	}
	pageArgs := append(append([]interface{}{}, args...), q.Limit, q.Offset)
	pageQ := productSelect + where + " ORDER BY p.sort_order, p.name" +
		fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(pageArgs)-1, len(pageArgs))

	rows, err := s.pool.Query(ctx, pageQ, pageArgs...)
	if err != nil {
		return nil, 0, err
	}
	var out []catalogapi.Product
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			rows.Close()
			return nil, 0, err
		}
		out = append(out, *p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	// Images and variants only — the console list shows a thumbnail and a
	// price, and modifier groups are detail-page weight.
	for i := range out {
		if err := s.loadListChildren(ctx, &out[i]); err != nil {
			return nil, 0, err
		}
	}
	return out, total, nil
}
