package product

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"bakehouse/pkg/strx"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/catalog/internal/store/shared"
)

func (s *Repo) CreateProduct(ctx context.Context, p catalogapi.Product) (*catalogapi.Product, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	const q = `
		INSERT INTO products (category_id, name, slug, description, base_price_cents, lead_time, lead_time_unit,
		production_station, allergen_flags, dietary_flags, occasion_tags, freshness_note,
		ingredients_note, returns_note, disclaimer, is_best_seller, sort_order, price_floor_cents, price_ceiling_cents,
		is_active, available_from, available_until, show_on_pos, show_on_website, pricing_mode,
		min_prep_time, min_prep_time_unit, max_prep_time, max_prep_time_unit, video_url)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30)
		RETURNING id`
	var id string
	err = tx.QueryRow(ctx, q, p.CategoryID, p.Name, p.Slug, p.Description, p.BasePriceCents, orDefault(p.LeadTime, 2), strx.OrDefault(p.LeadTimeUnit, "hours"),
		p.ProductionStation, strx.OrEmptySlice(p.AllergenFlags), strx.OrEmptySlice(p.DietaryFlags), strx.OrEmptySlice(p.OccasionTags),
		p.FreshnessNote, p.IngredientsNote, p.ReturnsNote, p.Disclaimer, p.IsBestSeller, p.SortOrder, p.PriceFloorCents, p.PriceCeilingCents,
		p.IsActive, p.AvailableFrom, p.AvailableUntil, p.ShowOnPos, p.ShowOnWebsite, p.EffectivePricingMode(),
		p.MinPrepTime, prepUnit(p.MinPrepTimeUnit), p.MaxPrepTime, prepUnit(p.MaxPrepTimeUnit), p.VideoURL).
		Scan(&id)
	if err != nil {
		// Classified rather than wrapped raw: a reused slug and a category id
		// that names nothing are both things the person filling the form can
		// fix, and both used to arrive as "an unexpected error occurred".
		if known := shared.Classify(err); known != err {
			return nil, known
		}
		return nil, fmt.Errorf("insert product: %w", err)
	}

	// When no branches are explicitly specified, default-map the new product
	// to every active branch so it is immediately available network-wide.
	// Callers that pass an explicit storeIds list (even an empty one) are
	// stating intent, so we only fill in the default when the field was
	// entirely absent (nil / zero-length slice).
	if len(p.BranchIds) == 0 {
		branchRows, err := tx.Query(ctx, `SELECT id FROM branches WHERE status = 'active'`)
		if err != nil {
			return nil, fmt.Errorf("fetch active branches for default mapping: %w", err)
		}
		defer branchRows.Close()
		for branchRows.Next() {
			var bid string
			if err := branchRows.Scan(&bid); err != nil {
				return nil, fmt.Errorf("scan active branch id: %w", err)
			}
			p.BranchIds = append(p.BranchIds, bid)
		}
		if err := branchRows.Err(); err != nil {
			return nil, fmt.Errorf("iterate active branches: %w", err)
		}
	}

	if err := writeProductChildren(ctx, tx, id, p); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetProductByID(ctx, id)
}

func (s *Repo) UpdateProduct(ctx context.Context, id string, p catalogapi.Product) (*catalogapi.Product, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	const q = `
		UPDATE products SET
			category_id=$2, name=$3, slug=$4, description=$5, base_price_cents=$6, lead_time=$7, lead_time_unit=$8,
			production_station=$9, allergen_flags=$10, dietary_flags=$11, occasion_tags=$12,
			freshness_note=$13, ingredients_note=$14, returns_note=$15, disclaimer=$16,
			is_best_seller=$17, sort_order=$18, price_floor_cents=$19, price_ceiling_cents=$20,
			is_active=$21, available_from=$22, available_until=$23,
			show_on_pos=$24, show_on_website=$25, pricing_mode=$26,
			min_prep_time=$27, min_prep_time_unit=$28, max_prep_time=$29, max_prep_time_unit=$30, video_url=$31, updated_at=NOW()
		WHERE id = $1 AND deleted_at IS NULL`
	tag, err := tx.Exec(ctx, q, id, p.CategoryID, p.Name, p.Slug, p.Description, p.BasePriceCents, orDefault(p.LeadTime, 2), strx.OrDefault(p.LeadTimeUnit, "hours"),
		p.ProductionStation, strx.OrEmptySlice(p.AllergenFlags), strx.OrEmptySlice(p.DietaryFlags), strx.OrEmptySlice(p.OccasionTags),
		p.FreshnessNote, p.IngredientsNote, p.ReturnsNote, p.Disclaimer, p.IsBestSeller, p.SortOrder, p.PriceFloorCents, p.PriceCeilingCents,
		p.IsActive, p.AvailableFrom, p.AvailableUntil, p.ShowOnPos, p.ShowOnWebsite, p.EffectivePricingMode(),
		p.MinPrepTime, prepUnit(p.MinPrepTimeUnit), p.MaxPrepTime, prepUnit(p.MaxPrepTimeUnit), p.VideoURL)
	if err != nil {
		if known := shared.Classify(err); known != err {
			return nil, known
		}
		return nil, fmt.Errorf("update product: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, shared.ErrNotFound
	}

	// Whole-collection replace, but only for collections actually present in
	// the request: a PATCH that omits "variants" leaves existing variants
	// untouched; a PATCH with an explicit "variants": [] clears them. Without
	// this nil-check, any partial update (e.g. price-band-only) would
	// silently delete every variant/modifier group/image on the product —
	// order line items keep their own denormalised snapshot regardless, so
	// this never rewrites order history either way.
	if p.Variants != nil {
		// A variant an order or a live cart points at cannot be deleted —
		// cart_items.variant_id and order_items.variant_id are NO ACTION
		// foreign keys, so the delete simply fails. Which meant editing a
		// product became impossible the moment anyone bought it: the console
		// reported "an unexpected error occurred" and nothing said why.
		//
		// So the unreferenced ones go, and the rest are retired instead:
		// still there for the history that points at them, no longer offered
		// to anyone. The rows the caller sent are inserted fresh below.
		if _, err := tx.Exec(ctx, `
			DELETE FROM product_variants pv
			 WHERE pv.product_id = $1
			   AND NOT EXISTS (SELECT 1 FROM cart_items ci WHERE ci.variant_id = pv.id)
			   AND NOT EXISTS (SELECT 1 FROM order_items oi WHERE oi.variant_id = pv.id)`, id); err != nil {
			return nil, fmt.Errorf("clear product variants: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE product_variants SET is_active = false WHERE product_id = $1`, id); err != nil {
			return nil, fmt.Errorf("retire referenced variants: %w", err)
		}
	}
	if p.ModifierGroups != nil {
		if _, err := tx.Exec(ctx, `DELETE FROM modifier_groups WHERE product_id = $1`, id); err != nil {
			return nil, err
		}
	}
	if p.Images != nil {
		if _, err := tx.Exec(ctx, `DELETE FROM product_images WHERE product_id = $1`, id); err != nil {
			return nil, err
		}
	}

	if err := writeProductChildren(ctx, tx, id, p); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetProductByID(ctx, id)
}

func writeProductChildren(ctx context.Context, tx pgx.Tx, productID string, p catalogapi.Product) error {
	for _, img := range p.Images {
		if _, err := tx.Exec(ctx, `
			INSERT INTO product_images (product_id, url, alt_text, type, sort_order, is_primary)
			VALUES ($1,$2,$3,$4,$5,$6)`,
			productID, img.URL, img.AltText, strx.OrDefault(img.Type, catalogapi.ImageStudio), img.SortOrder, img.IsPrimary); err != nil {
			return fmt.Errorf("insert product image: %w", err)
		}
	}
	for _, v := range p.Variants {
		if _, err := tx.Exec(ctx, `
			INSERT INTO product_variants (product_id, name, label, price_delta_cents, serves_min, serves_max, sort_order, is_active)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			productID, v.Name, v.Label, v.PriceDeltaCents, v.ServesMin, v.ServesMax, v.SortOrder, v.IsActive); err != nil {
			return fmt.Errorf("insert product variant: %w", err)
		}
	}
	for _, g := range p.ModifierGroups {
		var groupID string
		err := tx.QueryRow(ctx, `
			INSERT INTO modifier_groups (product_id, name, type, is_required, min_select, max_select, sort_order)
			VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
			productID, g.Name, g.Type, g.IsRequired, g.MinSelect, g.MaxSelect, g.SortOrder).Scan(&groupID)
		if err != nil {
			return fmt.Errorf("insert modifier group: %w", err)
		}
		for _, o := range g.Options {
			if _, err := tx.Exec(ctx, `
				INSERT INTO modifier_options (modifier_group_id, name, price_delta_cents, is_pre_selected, colour_hex, image_url, allergen_flags, is_gluten_free, sort_order, is_active)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
				groupID, o.Name, o.PriceDeltaCents, o.IsPreSelected, o.ColourHex, o.ImageURL, strx.OrEmptySlice(o.AllergenFlags), o.IsGlutenFree, o.SortOrder, o.IsActive); err != nil {
				return fmt.Errorf("insert modifier option: %w", err)
			}
		}
	}

	// StorePrices is nil when the field was absent from the PATCH request
	// (leave existing overrides untouched). A non-nil slice — including an
	// explicitly empty one — means "these are the new overrides": all other
	// branches must have their override reset to 0.
	if p.StorePrices != nil {
		// Step 1: zero out ALL existing price overrides for this product.
		if _, err := tx.Exec(ctx, `
			UPDATE branch_product_rules
			SET    price_override_cents = 0,
			       updated_at           = NOW()
			WHERE  product_id = $1
		`, productID); err != nil {
			return fmt.Errorf("reset branch product price overrides: %w", err)
		}

		// Step 2: upsert each entry that was actually sent.
		for _, sp := range p.StorePrices {
			branchID := strings.TrimSpace(sp.BranchID)
			if branchID == "" {
				continue
			}

			_, err := tx.Exec(ctx, `
				INSERT INTO branch_product_rules (
					branch_id,
					product_id,
					price_override_cents,
					updated_at
				)
				VALUES ($1, $2, $3, NOW())
				ON CONFLICT (branch_id, product_id)
				DO UPDATE SET
					price_override_cents = EXCLUDED.price_override_cents,
					updated_at           = NOW()
			`,
				branchID,
				productID,
				sp.PriceOverrideCents,
			)
			if err != nil {
				return fmt.Errorf("upsert branch product rule: %w", err)
			}
		}
	}

	// Disable all existing branches for this product.
	_, err := tx.Exec(ctx, `
    UPDATE branch_product_rules
    SET branch_enabled = false,
        updated_at = NOW()
    WHERE product_id = $1
`, productID)

	if err != nil {
		return fmt.Errorf("disable existing branch product rules: %w", err)
	}

	for _, branchID := range p.BranchIds {
		branchID = strings.TrimSpace(branchID)

		if branchID == "" {
			continue
		}

		_, err := tx.Exec(ctx, `
        INSERT INTO branch_product_rules (
            branch_id,
            product_id,
            branch_enabled,
            updated_at
        )
        VALUES ($1, $2, $3, NOW())
        ON CONFLICT (branch_id, product_id)
        DO UPDATE SET
            branch_enabled = EXCLUDED.branch_enabled,
            updated_at = NOW()
    `,
			branchID,
			productID,
			true,
		)

		if err != nil {
			return fmt.Errorf("upsert branch product rule: %w", err)
		}
	}

	return nil
}

// ReplaceProductVariants writes a product's size ladder.
//
// A variation is a choice that changes the price and has no picture — a size,
// a weight — which is exactly what product_variants is for, and why attaching
// one copies its options here rather than linking the group. Each product then
// owns its sizes and can be tuned without touching anyone else's.
//
// Variants an order or a live cart points at are retired rather than removed:
// cart_items.variant_id and order_items.variant_id are NO ACTION foreign keys,
// so deleting one fails outright, and an order must keep resolving the size it
// was placed with.
func (s *Repo) ReplaceProductVariants(ctx context.Context, productID string, variants []catalogapi.ProductVariant) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		DELETE FROM product_variants pv
		 WHERE pv.product_id = $1
		   AND NOT EXISTS (SELECT 1 FROM cart_items ci WHERE ci.variant_id = pv.id)
		   AND NOT EXISTS (SELECT 1 FROM order_items oi WHERE oi.variant_id = pv.id)`,
		productID); err != nil {
		return fmt.Errorf("clear product variants: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE product_variants SET is_active = false WHERE product_id = $1`, productID); err != nil {
		return fmt.Errorf("retire referenced variants: %w", err)
	}

	for i, v := range variants {
		if _, err := tx.Exec(ctx, `
			INSERT INTO product_variants (product_id, name, label, price_delta_cents, serves_min, serves_max, sort_order, is_active)
			VALUES ($1,$2,$3,$4,$5,$6,$7,true)`,
			productID, v.Name, v.Label, v.PriceDeltaCents, v.ServesMin, v.ServesMax, i); err != nil {
			return fmt.Errorf("insert variant %q: %w", v.Name, err)
		}
	}
	return tx.Commit(ctx)
}

func (s *Repo) SoftDeleteProduct(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE products SET deleted_at = NOW(), is_active = false, updated_at = NOW() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}

// AddPopularity raises popularity_score by qty for each product, in one
// statement.
//
// `+ delta` rather than `= total`: the rollup is incremental and several
// orders can be fulfilled at once, so a read-modify-write would lose whichever
// increment lost the race. Postgres serialises the addition per row.
//
// A product id that no longer exists (deleted since the order was placed)
// simply matches nothing — the update is silent about it rather than failing
// the whole batch for one stale line.
func (s *Repo) AddPopularity(ctx context.Context, productIDs []string, quantities []int32) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE products p
		SET popularity_score = p.popularity_score + v.qty,
		    updated_at = NOW()
		FROM (SELECT unnest($1::uuid[]) AS id, unnest($2::int[]) AS qty) v
		WHERE p.id = v.id AND p.deleted_at IS NULL`,
		productIDs, quantities)
	return err
}
