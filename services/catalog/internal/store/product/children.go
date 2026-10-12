package product

import (
	"context"
	"fmt"

	"bakehouse/services/catalog/catalogapi"
)

func (s *Repo) loadChildren(ctx context.Context, p *catalogapi.Product) error {
	if err := s.loadTags(ctx, p); err != nil {
		return err
	}
	imgRows, err := s.pool.Query(ctx, `SELECT id, url, alt_text, type, sort_order, is_primary FROM product_images WHERE product_id = $1 ORDER BY sort_order`, p.ID)
	if err != nil {
		return err
	}
	for imgRows.Next() {
		var img catalogapi.ProductImage
		img.ProductID = p.ID
		if err := imgRows.Scan(&img.ID, &img.URL, &img.AltText, &img.Type, &img.SortOrder, &img.IsPrimary); err != nil {
			imgRows.Close()
			return err
		}
		p.Images = append(p.Images, img)
	}
	imgRows.Close()

	varRows, err := s.pool.Query(ctx, `SELECT id, name, label, price_delta_cents, serves_min, serves_max, sort_order, is_active FROM product_variants WHERE product_id = $1 AND is_active = true ORDER BY sort_order`, p.ID)
	if err != nil {
		return err
	}
	for varRows.Next() {
		var v catalogapi.ProductVariant
		v.ProductID = p.ID
		if err := varRows.Scan(&v.ID, &v.Name, &v.Label, &v.PriceDeltaCents, &v.ServesMin, &v.ServesMax, &v.SortOrder, &v.IsActive); err != nil {
			varRows.Close()
			return err
		}
		p.Variants = append(p.Variants, v)
	}
	varRows.Close()

	if err := s.loadModifierGroups(ctx, p); err != nil {
		return err
	}

	// Load branch/product rules — only real price overrides in StorePrices, but all enabled branches in BranchIds
	ruleRows, err := s.pool.Query(ctx, `
		SELECT
			bpr.branch_id,
			COALESCE(b.name, ''),
			bpr.branch_enabled,
			bpr.price_override_cents,
			bpr.available_days,
			bpr.date_overrides,
			bpr.available_from,
			bpr.available_until,
			bpr.reason,
			bpr.updated_by,
			bpr.updated_at
		FROM branch_product_rules bpr
		LEFT JOIN branches b ON b.id = bpr.branch_id
		WHERE bpr.product_id = $1
		ORDER BY bpr.price_override_cents ASC, bpr.branch_id ASC
	`, p.ID)
	if err != nil {
		return err
	}
	defer ruleRows.Close()

	rules := make([]catalogapi.BranchProductRule, 0)
	p.BranchIds = make([]string, 0)

	for ruleRows.Next() {
		var rule catalogapi.BranchProductRule

		rule.ProductID = p.ID

		if err := ruleRows.Scan(
			&rule.BranchID,
			&rule.BranchName,
			&rule.BranchEnabled,
			&rule.PriceOverrideCents,
			&rule.AvailableDays,
			&rule.DateOverrides,
			&rule.AvailableFrom,
			&rule.AvailableUntil,
			&rule.Reason,
			&rule.UpdatedBy,
			&rule.UpdatedAt,
		); err != nil {
			return err
		}

		if rule.BranchEnabled {
			p.BranchIds = append(p.BranchIds, rule.BranchID)
		}
		if rule.PriceOverrideCents != nil && *rule.PriceOverrideCents != 0 {
			rules = append(rules, rule)
		}
	}

	if err := ruleRows.Err(); err != nil {
		return err
	}

	p.StorePrices = rules
	return nil
}

func (s *Repo) loadListChildren(ctx context.Context, p *catalogapi.Product) error {
	if err := s.loadTags(ctx, p); err != nil {
		return err
	}
	imgRows, err := s.pool.Query(ctx, `SELECT id, url, alt_text, type, sort_order, is_primary FROM product_images WHERE product_id = $1 ORDER BY sort_order`, p.ID)
	if err != nil {
		return err
	}
	for imgRows.Next() {
		var img catalogapi.ProductImage
		if err := imgRows.Scan(&img.ID, &img.URL, &img.AltText, &img.Type, &img.SortOrder, &img.IsPrimary); err != nil {
			imgRows.Close()
			return err
		}
		p.Images = append(p.Images, img)
	}
	imgRows.Close()

	varRows, err := s.pool.Query(ctx, `SELECT id, name, label, price_delta_cents, serves_min, serves_max, sort_order, is_active FROM product_variants WHERE product_id = $1 AND is_active = true ORDER BY sort_order`, p.ID)
	if err != nil {
		return err
	}
	for varRows.Next() {
		var v catalogapi.ProductVariant
		if err := varRows.Scan(&v.ID, &v.Name, &v.Label, &v.PriceDeltaCents, &v.ServesMin, &v.ServesMax, &v.SortOrder, &v.IsActive); err != nil {
			varRows.Close()
			return err
		}
		p.Variants = append(p.Variants, v)
	}
	varRows.Close()

	// Load branch/product rules — only real price overrides in StorePrices, but all enabled branches in BranchIds
	ruleRows, err := s.pool.Query(ctx, `
		SELECT
			bpr.branch_id,
			COALESCE(b.name, ''),
			bpr.branch_enabled,
			bpr.price_override_cents,
			bpr.available_days,
			bpr.date_overrides,
			bpr.available_from,
			bpr.available_until,
			bpr.reason,
			bpr.updated_by,
			bpr.updated_at
		FROM branch_product_rules bpr
		LEFT JOIN branches b ON b.id = bpr.branch_id
		WHERE bpr.product_id = $1
		ORDER BY bpr.price_override_cents ASC, bpr.branch_id ASC
	`, p.ID)
	if err != nil {
		return err
	}
	defer ruleRows.Close()

	rules := make([]catalogapi.BranchProductRule, 0)
	p.BranchIds = make([]string, 0)

	for ruleRows.Next() {
		var rule catalogapi.BranchProductRule

		rule.ProductID = p.ID

		if err := ruleRows.Scan(
			&rule.BranchID,
			&rule.BranchName,
			&rule.BranchEnabled,
			&rule.PriceOverrideCents,
			&rule.AvailableDays,
			&rule.DateOverrides,
			&rule.AvailableFrom,
			&rule.AvailableUntil,
			&rule.Reason,
			&rule.UpdatedBy,
			&rule.UpdatedAt,
		); err != nil {
			return err
		}

		if rule.BranchEnabled {
			p.BranchIds = append(p.BranchIds, rule.BranchID)
		}
		if rule.PriceOverrideCents != nil && *rule.PriceOverrideCents != 0 {
			rules = append(rules, rule)
		}
	}
	if err := ruleRows.Err(); err != nil {
		return err
	}

	p.StorePrices = rules

	// A variation-priced product has no meaningful price without its
	// variations, so a list card (POS grid, website grid) needs the groups
	// to show one. Product-priced items keep the light shape — their groups
	// are detail-page weight, and the base price stands on its own.
	if p.IsVariationPriced() {
		if err := s.loadModifierGroups(ctx, p); err != nil {
			return err
		}
	}

	// Compute MaxModifierDeltaCents: sum of the highest-priced option per
	// modifier group. Used by storefront list cards to display a price range
	// without loading full modifier groups.
	var maxDelta int64
	_ = s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(group_max), 0)
		FROM (
			SELECT MAX(o.price_delta_cents) AS group_max
			FROM modifier_groups g
			LEFT JOIN product_modifier_groups pmg ON pmg.modifier_group_id = g.id AND pmg.product_id = $1
			JOIN modifier_options o ON o.modifier_group_id = g.id AND o.is_active = true
			WHERE (g.product_id = $1 OR pmg.product_id = $1)
			  AND o.price_delta_cents > 0
			GROUP BY g.id
		) sub
	`, p.ID).Scan(&maxDelta)
	p.MaxModifierDeltaCents = maxDelta

	return ruleRows.Err()
}

// loadModifierGroups attaches every modifier group the product carries,
// with active options and any branch price overrides on those options.
//
// Both shapes at once: groups the product owns outright (the original
// model), and library groups applied to it through
// product_modifier_groups. A product page must show either kind, and
// reading only the first is why a modifier applied from the console
// appeared in admin but never reached the storefront.
//
// COALESCE on max_select because the column became nullable when "no
// maximum" had to be expressible; the storefront treats 0 as unbounded.
func (s *Repo) loadModifierGroups(ctx context.Context, p *catalogapi.Product) error {
	grpRows, err := s.pool.Query(ctx, `
		SELECT g.id, g.name, COALESCE(g.display_name, ''), g.type, g.kind, g.is_required, g.min_select,
		       COALESCE(g.max_select, 0), COALESCE(pmg.sort_order, g.sort_order) AS sort_order
		  FROM modifier_groups g
		  LEFT JOIN product_modifier_groups pmg
		         ON pmg.modifier_group_id = g.id AND pmg.product_id = $1
		 WHERE g.product_id = $1 OR pmg.product_id = $1
		 ORDER BY sort_order`, p.ID)
	if err != nil {
		return err
	}
	var groups []catalogapi.ModifierGroup
	for grpRows.Next() {
		var g catalogapi.ModifierGroup
		g.ProductID = p.ID
		if err := grpRows.Scan(&g.ID, &g.Name, &g.DisplayName, &g.Type, &g.Kind, &g.IsRequired, &g.MinSelect, &g.MaxSelect, &g.SortOrder); err != nil {
			grpRows.Close()
			return err
		}
		groups = append(groups, g)
	}
	grpRows.Close()

	for i := range groups {
		optRows, err := s.pool.Query(ctx, `
			SELECT id, name, price_delta_cents, is_pre_selected, colour_hex, image_url, allergen_flags, is_gluten_free, sort_order, is_active
			FROM modifier_options WHERE modifier_group_id = $1 AND is_active = true ORDER BY sort_order`, groups[i].ID)
		if err != nil {
			return err
		}
		for optRows.Next() {
			var o catalogapi.ModifierOption
			o.GroupID = groups[i].ID
			if err := optRows.Scan(&o.ID, &o.Name, &o.PriceDeltaCents, &o.IsPreSelected, &o.ColourHex, &o.ImageURL, &o.AllergenFlags, &o.IsGlutenFree, &o.SortOrder, &o.IsActive); err != nil {
				optRows.Close()
				return err
			}
			groups[i].Options = append(groups[i].Options, o)
		}
		optRows.Close()
	}

	var allOptionIDs []string
	for i := range groups {
		for j := range groups[i].Options {
			allOptionIDs = append(allOptionIDs, groups[i].Options[j].ID)
		}
	}
	if len(allOptionIDs) > 0 {
		ruleRows, err := s.pool.Query(ctx, `
			SELECT bmr.id, bmr.branch_id, COALESCE(b.name, ''), bmr.modifier_group_id, bmr.modifier_option_id, bmr.price_override_cents,
			       bmr.available_from, bmr.available_until, bmr.reason, bmr.updated_by, bmr.updated_at
			FROM branch_modifier_rules bmr
			LEFT JOIN branches b ON b.id = bmr.branch_id
			WHERE bmr.modifier_option_id = ANY($1)
			  AND bmr.price_override_cents IS NOT NULL
			  AND bmr.price_override_cents != 0
			ORDER BY bmr.price_override_cents ASC, bmr.branch_id ASC`, allOptionIDs)
		if err == nil {
			defer ruleRows.Close()
			rulesByOption := make(map[string][]catalogapi.BranchModifierRule)
			for ruleRows.Next() {
				var r catalogapi.BranchModifierRule
				if err := ruleRows.Scan(&r.ID, &r.BranchID, &r.BranchName, &r.ModifierGroupID, &r.ModifierOptionID,
					&r.PriceOverrideCents, &r.AvailableFrom, &r.AvailableUntil, &r.Reason,
					&r.UpdatedBy, &r.UpdatedAt); err == nil {
					rulesByOption[r.ModifierOptionID] = append(rulesByOption[r.ModifierOptionID], r)
				}
			}
			for i := range groups {
				for j := range groups[i].Options {
					groups[i].Options[j].StorePrices = rulesByOption[groups[i].Options[j].ID]
				}
			}
		}
	}

	p.ModifierGroups = groups
	p.IsModifier = len(groups) > 0
	return nil
}

// loadListChildren attaches the images and variants a product card needs.
// Modifier groups are deliberately skipped -- they are detail-page weight and
// would multiply the query count across a full grid.
// loadTags reads the product's seasonal tags.
func (s *Repo) loadTags(ctx context.Context, p *catalogapi.Product) error {
	tags, err := s.tags.TagsForProduct(ctx, p.ID)
	if err != nil {
		return fmt.Errorf("load product tags: %w", err)
	}
	p.SeasonalTags = tags
	return nil
}
