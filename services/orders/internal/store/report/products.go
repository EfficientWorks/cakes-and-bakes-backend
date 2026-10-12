package report

import (
	"context"
	"fmt"

	"bakehouse/services/orders/ordersapi"
)

func (s *Repo) TopCategoriesForRange(ctx context.Context, branchID, from, to string, limit int) ([]ordersapi.CategorySales, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT pc.id::text, pc.name,
		       SUM(`+keptQtySQL+`)::int,
		       COALESCE(SUM(`+keptAmountExAddonsSQL+`), 0)::bigint
		  FROM order_items oi
		  JOIN orders o              ON o.id = oi.order_id
		  JOIN branches b            ON b.id = o.branch_id
		  JOIN products p            ON p.id = oi.product_id
		  JOIN product_categories pc ON pc.id = p.category_id
		 WHERE o.branch_id = $1
		   AND o.deleted_at IS NULL
		   AND `+settledOrderSQL+`
		   AND `+liveLineSQL+`
		   AND (`+orderTimeSQL+` AT TIME ZONE b.timezone)::date BETWEEN $2::date AND $3::date
		 GROUP BY pc.id, pc.name
		 HAVING SUM(`+keptQtySQL+`) > 0
		 ORDER BY 4 DESC
		 LIMIT $4`, branchID, from, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ordersapi.CategorySales{}
	for rows.Next() {
		var c ordersapi.CategorySales
		if err := rows.Scan(&c.CategoryID, &c.CategoryName, &c.Quantity, &c.AmountCents); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// TopProducts returns the best sellers over a range, biggest first.
//
// Counted from the lines of settled orders (fulfilled and paid), net of
// returns and skipping lines cancelled at the till — so a product's quantity
// is what actually left the shop and was paid for, consolidated across every
// such order. Each product carries three breakdowns where they apply: the
// catalogue variant it was sold as, the variations (flavours) chosen, and
// the add-ons chosen.
func (s *Repo) TopProducts(ctx context.Context, branchID, from, to string, limit int) ([]ordersapi.ProductSales, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT COALESCE(oi.product_id::text, ''), MIN(oi.product_name),
		       SUM(`+keptQtySQL+`)::int,
		       COALESCE(SUM(`+keptAmountExAddonsSQL+`), 0)::bigint
		  FROM order_items oi
		  JOIN orders o   ON o.id = oi.order_id
		  JOIN branches b ON b.id = o.branch_id
		 WHERE o.branch_id = $1
		   AND o.deleted_at IS NULL
		   AND `+settledOrderSQL+`
		   AND `+liveLineSQL+`
		   AND (`+orderTimeSQL+` AT TIME ZONE b.timezone)::date BETWEEN $2::date AND $3::date
		 GROUP BY oi.product_id
		 HAVING SUM(`+keptQtySQL+`) > 0
		 ORDER BY 4 DESC
		 LIMIT $4`, branchID, from, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ordersapi.ProductSales{}
	productIDs := []string{}
	for rows.Next() {
		var p ordersapi.ProductSales
		if err := rows.Scan(&p.ProductID, &p.ProductName, &p.Quantity, &p.AmountCents); err != nil {
			return nil, err
		}
		out = append(out, p)
		if p.ProductID != "" {
			productIDs = append(productIDs, p.ProductID)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(productIDs) == 0 {
		return out, nil
	}

	variantRows, err := s.pool.Query(ctx, `
		SELECT oi.product_id::text, COALESCE(oi.variant_id::text, ''),
		       COALESCE(NULLIF(oi.variant_name, ''), 'Regular'),
		       SUM(`+keptQtySQL+`)::int,
		       COALESCE(SUM(`+keptAmountExAddonsSQL+`), 0)::bigint
		  FROM order_items oi
		  JOIN orders o   ON o.id = oi.order_id
		  JOIN branches b ON b.id = o.branch_id
		 WHERE o.branch_id = $1
		   AND o.deleted_at IS NULL
		   AND oi.product_id::text = ANY($2)
		   AND `+settledOrderSQL+`
		   AND `+liveLineSQL+`
		   AND (`+orderTimeSQL+` AT TIME ZONE b.timezone)::date BETWEEN $3::date AND $4::date
		 GROUP BY oi.product_id, oi.variant_id, oi.variant_name
		 HAVING SUM(`+keptQtySQL+`) > 0
		 ORDER BY 5 DESC`, branchID, productIDs, from, to)
	if err != nil {
		return nil, err
	}
	defer variantRows.Close()

	byProduct := map[string][]ordersapi.ProductVariantSales{}
	for variantRows.Next() {
		var v ordersapi.ProductVariantSales
		var productID string
		if err := variantRows.Scan(&productID, &v.VariantID, &v.VariantName, &v.Quantity, &v.AmountCents); err != nil {
			return nil, err
		}
		byProduct[productID] = append(byProduct[productID], v)
	}
	if err := variantRows.Err(); err != nil {
		return nil, err
	}

	// Chosen variations live as JSON on the line (selected_flavours holds
	// the "variation" kind of modifier). Add-ons are left out of the
	// best-seller list altogether, in the count and in the money.
	variations, err := s.topProductModifiers(ctx, branchID, productIDs, from, to, "selected_flavours")
	if err != nil {
		return nil, err
	}

	/*
	 * A product carries its variant breakdown whenever it was sold as a
	 * catalogue variant at all — one variant included. It used to need two,
	 * which hid the variation on every product sold in a single size that
	 * day. A product with no variants (every row has an empty variant_id)
	 * gets no list, rather than a one-line "Regular" that says nothing.
	 */
	for i := range out {
		variants := byProduct[out[i].ProductID]
		hasRealVariant := false
		for _, v := range variants {
			if v.VariantID != "" {
				hasRealVariant = true
				break
			}
		}
		if hasRealVariant {
			out[i].Variants = variants
		}
		out[i].Variations = variations[out[i].ProductID]
	}
	return out, nil
}

// topProductModifiers unnests one of the JSON option columns on order_items
// and totals each chosen option per product: how many units were sold with
// it, and what it added to the bill (its price delta times those units).
// column must be one of the two literal JSONB column names; it is spliced
// into the query, so it is never taken from a caller outside this file.
func (s *Repo) topProductModifiers(ctx context.Context, branchID string, productIDs []string, from, to, column string) (map[string][]ordersapi.ProductModifierSales, error) {
	if column != "selected_flavours" && column != "selected_addons" {
		return nil, fmt.Errorf("topProductModifiers: unexpected column %q", column)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT oi.product_id::text,
		       COALESCE(m->>'id', ''),
		       COALESCE(NULLIF(m->>'name', ''), 'Unnamed'),
		       SUM(`+keptQtySQL+`)::int,
		       COALESCE(SUM(COALESCE((m->>'priceDelta')::bigint, 0) * `+keptQtySQL+`), 0)::bigint
		  FROM order_items oi
		  JOIN orders o   ON o.id = oi.order_id
		  JOIN branches b ON b.id = o.branch_id
		  CROSS JOIN LATERAL jsonb_array_elements(
		      CASE WHEN jsonb_typeof(oi.`+column+`) = 'array' THEN oi.`+column+` ELSE '[]'::jsonb END) AS m
		 WHERE o.branch_id = $1
		   AND o.deleted_at IS NULL
		   AND oi.product_id::text = ANY($2)
		   AND `+settledOrderSQL+`
		   AND `+liveLineSQL+`
		   AND (`+orderTimeSQL+` AT TIME ZONE b.timezone)::date BETWEEN $3::date AND $4::date
		 GROUP BY oi.product_id, m->>'id', m->>'name'
		 HAVING SUM(`+keptQtySQL+`) > 0
		 ORDER BY 4 DESC, 3`, branchID, productIDs, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string][]ordersapi.ProductModifierSales{}
	for rows.Next() {
		var productID string
		var m ordersapi.ProductModifierSales
		if err := rows.Scan(&productID, &m.ID, &m.Name, &m.Quantity, &m.AmountCents); err != nil {
			return nil, err
		}
		out[productID] = append(out[productID], m)
	}
	return out, rows.Err()
}

func (s *Repo) ReturnItemsForRange(ctx context.Context, branchID, from, to string) ([]ordersapi.ReturnItemLine, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT o.id::text, o.reference, COALESCE(oi.product_id::text, ''), oi.product_name,
		       oi.returned_quantity,
		       (oi.returned_quantity * oi.unit_price_cents)::bigint,
		       o.status
		  FROM order_items oi
		  JOIN orders o   ON o.id = oi.order_id
		  JOIN branches b ON b.id = o.branch_id
		 WHERE o.branch_id = $1
		   AND o.deleted_at IS NULL
		   AND oi.returned_quantity > 0
		   AND o.status IN ('fulfilled','cancelled','out_for_delivery','returned')
		   AND (`+orderTimeSQL+` AT TIME ZONE b.timezone)::date BETWEEN $2::date AND $3::date
		 ORDER BY o.created_at DESC`, branchID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ordersapi.ReturnItemLine{}
	for rows.Next() {
		var r ordersapi.ReturnItemLine
		if err := rows.Scan(&r.OrderID, &r.OrderReference, &r.ProductID, &r.ProductName,
			&r.ReturnedQuantity, &r.AmountCents, &r.OrderStatus); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
