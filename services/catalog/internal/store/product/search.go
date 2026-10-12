package product

import (
	"context"
	"fmt"

	"bakehouse/pkg/tenant"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/catalog/internal/store/shared"
)

// SearchProducts returns one page of the storefront grid plus the total number
// of matches, which the grid needs for its result count and pager.
func (s *Repo) SearchProducts(ctx context.Context, f catalogapi.ProductFilter) ([]catalogapi.Product, int, error) {
	f = f.Normalise()
	where, args, priceLo := productFilterWhere(f, tenant.BusinessParam(ctx))

	countQ := `SELECT count(*) FROM products p JOIN product_categories c ON c.id = p.category_id
	           WHERE p.deleted_at IS NULL` + where
	var total int
	if err := s.pool.QueryRow(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count products: %w", err)
	}
	if total == 0 {
		return nil, 0, nil
	}

	pageArgs := append(append([]interface{}{}, args...), f.Limit, f.Offset())
	pageQ := productSelect + where + sortClause(f.Sort, priceLo) +
		fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(pageArgs)-1, len(pageArgs))

	rows, err := s.pool.Query(ctx, pageQ, pageArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("query products: %w", err)
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

	// Images and variants only — modifier groups are detail-page weight.
	for i := range out {
		if err := s.loadListChildren(ctx, &out[i]); err != nil {
			return nil, 0, err
		}
	}
	return out, total, nil
}

// DistinctFacets returns the occasion tags and dietary flags actually present
// on active products, so the filter panel offers only chips that can return a
// result rather than a hardcoded list.
func (s *Repo) DistinctFacets(ctx context.Context) (occasions, dietary []string, err error) {
	const q = `
		SELECT
		  (SELECT coalesce(array_agg(DISTINCT t), '{}') FROM products, unnest(occasion_tags) t
		    WHERE deleted_at IS NULL AND is_active = true AND business_id IS NOT DISTINCT FROM $1),
		  (SELECT coalesce(array_agg(DISTINCT d), '{}') FROM products, unnest(dietary_flags) d
		    WHERE deleted_at IS NULL AND is_active = true AND business_id IS NOT DISTINCT FROM $1)`
	if err := s.pool.QueryRow(ctx, q, tenant.BusinessParam(ctx)).Scan(&occasions, &dietary); err != nil {
		return nil, nil, fmt.Errorf("load product facets: %w", err)
	}
	return occasions, dietary, nil
}

// PriceBounds returns the lowest and highest price across active products,
// which sets the ends of the price slider. Uses the same pricing-mode-aware
// figures as the filter, so the slider's ends are values the filter can hit.
func (s *Repo) PriceBounds(ctx context.Context) (minCents, maxCents int64, err error) {
	lo, hi := productPriceExprs("")
	q := `SELECT coalesce(min(` + lo + `), 0)::bigint, coalesce(max(` + hi + `), 0)::bigint
	      FROM products p WHERE p.deleted_at IS NULL AND p.is_active = true AND p.business_id IS NOT DISTINCT FROM $1`
	if err := s.pool.QueryRow(ctx, q, tenant.BusinessParam(ctx)).Scan(&minCents, &maxCents); err != nil {
		return 0, 0, fmt.Errorf("load price bounds: %w", err)
	}
	return minCents, maxCents, nil
}

// productFilterWhere builds the shared WHERE fragment for the storefront grid
// and returns it with its arguments, so the page query and the count query
// cannot drift apart. The third value is the SQL for the product's starting
// price, bound to the same arguments, for the price sorts.
func productFilterWhere(f catalogapi.ProductFilter, business *string) (string, []interface{}, string) {
	// Only this business's products, only live ones, and only in a live
	// category: hiding a category alone would leave its products reachable
	// from search and "you may also love".
	q := " AND p.business_id IS NOT DISTINCT FROM $1" +
		" AND p.is_active = true" +
		" AND c.is_active = true"
	args := []interface{}{business}

	if f.Search != "" {
		// Escape the LIKE metacharacters so "50%" or "a_b" typed at the till
		// search for those characters rather than acting as wildcards.
		pattern := "%" + shared.LikeEscape(f.Search) + "%"
		args = append(args, pattern)
		if f.SearchNameOnly {
			q += fmt.Sprintf(" AND p.name ILIKE $%d ESCAPE '\\'", len(args))
		} else {
			// ILIKE rides the trigram indexes on both columns.
			q += fmt.Sprintf(" AND (p.name ILIKE $%d ESCAPE '\\' OR p.description ILIKE $%d ESCAPE '\\')", len(args), len(args))
		}
	}
	if f.CategoryID != "" {
		args = append(args, f.CategoryID)
		q += fmt.Sprintf(" AND p.category_id = $%d", len(args))
	} else if f.CategorySlug != "" {
		args = append(args, f.CategorySlug)
		q += fmt.Sprintf(" AND c.slug = $%d", len(args))
	}
	if len(f.Occasions) > 0 {
		// Overlap: any selected occasion matches.
		args = append(args, f.Occasions)
		q += fmt.Sprintf(" AND p.occasion_tags && $%d", len(args))
	}
	if len(f.TagIDs) > 0 {
		// Any selected seasonal tag, while it is switched on.
		args = append(args, f.TagIDs)
		q += fmt.Sprintf(` AND EXISTS (SELECT 1 FROM product_seasonal_tags pt JOIN seasonal_tags st ON st.id = pt.tag_id
			WHERE pt.product_id = p.id AND st.status = 'active' AND st.deleted_at IS NULL AND st.id::text = ANY($%d))`, len(args))
	}
	if len(f.Dietary) > 0 {
		// Contains: every selected dietary flag must be present.
		args = append(args, f.Dietary)
		q += fmt.Sprintf(" AND p.dietary_flags @> $%d", len(args))
	}
	// The branch is bound only when something prices with it. The count
	// query shares these args but not the ORDER BY, so a branch used only by
	// a price sort is also referenced here — a no-op, since it is never null —
	// to keep every bound parameter used by both queries.
	branchArg := ""
	priceSort := f.Sort == catalogapi.SortPriceAsc || f.Sort == catalogapi.SortPriceDesc
	if f.PriceBranchID != "" && (f.MinPriceCents != nil || f.MaxPriceCents != nil || priceSort) {
		args = append(args, f.PriceBranchID)
		branchArg = fmt.Sprintf("$%d", len(args))
		q += fmt.Sprintf(" AND %s::text IS NOT NULL", branchArg)
	}
	priceLo, priceHi := productPriceExprs(branchArg)
	// Overlap, not containment: a $10–$40 bouquet is a match for "under $13"
	// because it can be bought for $10, and for "over $30" because it can be
	// bought for $40. For a single-priced product both sides are that price.
	if f.MinPriceCents != nil {
		args = append(args, *f.MinPriceCents)
		q += fmt.Sprintf(" AND %s >= $%d", priceHi, len(args))
	}
	if f.MaxPriceCents != nil {
		args = append(args, *f.MaxPriceCents)
		q += fmt.Sprintf(" AND %s <= $%d", priceLo, len(args))
	}
	if f.BestSellerOnly {
		q += " AND p.is_best_seller = true"
	}
	if f.MappedBranchID != "" {
		args = append(args, f.MappedBranchID)
		q += fmt.Sprintf(`
			AND EXISTS (SELECT 1 FROM branch_product_rules bpr
			             WHERE bpr.product_id = p.id AND bpr.branch_id = $%d AND bpr.branch_enabled)
			AND EXISTS (SELECT 1 FROM branch_category_rules bcr
			             WHERE bcr.category_id = p.category_id AND bcr.branch_id = $%d AND bcr.branch_enabled)`,
			len(args), len(args))
	}
	if f.ShowOnPos != nil {
		if *f.ShowOnPos {
			q += " AND p.show_on_pos = true"
		} else {
			q += " AND p.show_on_pos = false"
		}
	}
	if f.ShowOnWebsite != nil {
		if *f.ShowOnWebsite {
			q += " AND p.show_on_website = true"
		} else {
			q += " AND p.show_on_website = false"
		}
	}
	return q, args, priceLo
}

// productPriceExprs returns SQL for the lowest and highest price a shopper can
// pay for product p (alias p), before add-ons, following its pricing mode —
// the same figures catalogapi.Product.PriceRange and the list DTO use.
//
//   - product mode: the base price, or the branch's override when branchArg
//     is set and the override is non-zero. Low and high are the same.
//   - variation mode: the sum over variation groups of the cheapest (low) and
//     dearest (high) active option, each option priced with the branch's
//     override when there is one. A variation-priced product with no
//     variation options falls back to its base price, as PriceRange does.
//
// branchArg is a bind placeholder such as "$4", or "" for no branch.
func productPriceExprs(branchArg string) (lo, hi string) {
	base := "p.base_price_cents"
	optPrice := "o.price_delta_cents"
	optJoin := ""
	if branchArg != "" {
		base = `COALESCE((SELECT NULLIF(bpr.price_override_cents, 0) FROM branch_product_rules bpr
		                   WHERE bpr.product_id = p.id AND bpr.branch_id::text = ` + branchArg + `), p.base_price_cents)`
		optPrice = "COALESCE(NULLIF(bmr.price_override_cents, 0), o.price_delta_cents)"
		optJoin = ` LEFT JOIN branch_modifier_rules bmr
		              ON bmr.modifier_option_id = o.id AND bmr.branch_id::text = ` + branchArg
	}
	variation := func(agg string) string {
		return `(SELECT SUM(gp) FROM (
			SELECT ` + agg + `(` + optPrice + `) AS gp
			  FROM modifier_groups g
			  JOIN modifier_options o ON o.modifier_group_id = g.id AND o.is_active = true` + optJoin + `
			 WHERE g.kind = 'variation'
			   AND (g.product_id = p.id OR EXISTS (SELECT 1 FROM product_modifier_groups pmg
			                                        WHERE pmg.modifier_group_id = g.id AND pmg.product_id = p.id))
			 GROUP BY g.id) vg)`
	}
	pick := func(agg string) string {
		return `(CASE WHEN p.pricing_mode = 'variation' THEN COALESCE(` + variation(agg) + `, ` + base + `) ELSE ` + base + ` END)`
	}
	return pick("MIN"), pick("MAX")
}

// sortClause maps a filter sort to SQL. Every branch ends in a deterministic
// tiebreaker so paging cannot repeat or drop a row between pages. priceLo is
// the SQL for the product's starting price (see productPriceExprs), so price
// sorts order by what the grid shows rather than a base price a
// variation-priced product does not charge.
func sortClause(sort, priceLo string) string {
	if priceLo == "" {
		priceLo = "p.base_price_cents"
	}
	switch sort {
	case catalogapi.SortPriceAsc:
		return " ORDER BY " + priceLo + " ASC, p.id"
	case catalogapi.SortPriceDesc:
		return " ORDER BY " + priceLo + " DESC, p.id"
	case catalogapi.SortNewest:
		return " ORDER BY p.created_at DESC, p.id"
	case catalogapi.SortNameAsc:
		return " ORDER BY p.name ASC, p.id"
	default: // SortMostGifted
		return " ORDER BY p.popularity_score DESC, p.is_best_seller DESC, p.sort_order, p.id"
	}
}
