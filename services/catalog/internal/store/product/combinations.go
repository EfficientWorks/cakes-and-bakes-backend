package product

import (
	"context"
	"fmt"

	"bakehouse/services/catalog/catalogapi"
)

// ListProductCombinations returns a product's option-set rows in display
// order. The caller has already confirmed the product belongs to the
// business, so the product id is the only scope needed.
func (s *Repo) ListProductCombinations(ctx context.Context, productID string) ([]catalogapi.ProductCombination, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, option_ids::text[], price_cents, is_active, sort_order
		  FROM product_combinations
		 WHERE product_id = $1
		 ORDER BY sort_order, created_at`, productID)
	if err != nil {
		return nil, fmt.Errorf("list product combinations: %w", err)
	}
	defer rows.Close()
	var out []catalogapi.ProductCombination
	for rows.Next() {
		var c catalogapi.ProductCombination
		if err := rows.Scan(&c.ID, &c.OptionIDs, &c.PriceOverrideCents, &c.IsActive, &c.SortOrder); err != nil {
			return nil, err
		}
		c.OptionIDs = catalogapi.NormaliseOptionIDs(c.OptionIDs)
		out = append(out, c)
	}
	return out, rows.Err()
}

// ReplaceProductCombinations makes the given rows the product's whole set, in
// one transaction. Rows are matched on their options, so a row that survives
// an edit keeps its id; rows not in the new set are deleted.
func (s *Repo) ReplaceProductCombinations(ctx context.Context, productID string, combos []catalogapi.ProductCombination) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	keep := make([]string, 0, len(combos))
	for i, c := range combos {
		var id string
		if err := tx.QueryRow(ctx, `
			INSERT INTO product_combinations (product_id, option_ids, price_cents, is_active, sort_order)
			VALUES ($1, $2::uuid[], $3, $4, $5)
			ON CONFLICT (product_id, option_ids) DO UPDATE
			   SET price_cents = EXCLUDED.price_cents,
			       is_active   = EXCLUDED.is_active,
			       sort_order  = EXCLUDED.sort_order,
			       updated_at  = now()
			RETURNING id::text`,
			productID, catalogapi.NormaliseOptionIDs(c.OptionIDs), c.PriceOverrideCents, c.IsActive, i).Scan(&id); err != nil {
			return fmt.Errorf("save product combination: %w", err)
		}
		keep = append(keep, id)
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM product_combinations WHERE product_id = $1 AND NOT (id::text = ANY($2))`,
		productID, keep); err != nil {
		return fmt.Errorf("prune product combinations: %w", err)
	}
	// A product sold by combination is priced by what was picked, so it is
	// variation-priced: that is also what makes list pages (shop grid, POS
	// grid) load its groups and rows to show a "From" price.
	if len(combos) > 0 {
		if _, err := tx.Exec(ctx,
			`UPDATE products SET pricing_mode = 'variation', updated_at = now() WHERE id = $1 AND pricing_mode <> 'variation'`,
			productID); err != nil {
			return fmt.Errorf("set pricing mode: %w", err)
		}
	}
	return tx.Commit(ctx)
}
