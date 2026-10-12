package modifier

import (
	"context"
	"fmt"

	"bakehouse/services/catalog/catalogapi"
)

// ListModifierGroups returns the library with its options and the products
// each group is applied to.
//
// Three queries rather than one join: a join across groups, options and the
// product link table multiplies rows by both children, and reassembling that
// is more code — and more chances to double-count an option — than reading
// each level and stitching them by id.
func (s *Repo) ListModifierGroups(ctx context.Context) ([]catalogapi.ModifierGroup, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, COALESCE(product_id::text, ''), name, COALESCE(display_name, name), kind, type,
		       is_required, allow_multiple, min_select, COALESCE(max_select, 0), sort_order
		FROM modifier_groups
		ORDER BY sort_order, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []catalogapi.ModifierGroup
	byID := map[string]int{}
	for rows.Next() {
		var g catalogapi.ModifierGroup
		if err := rows.Scan(&g.ID, &g.ProductID, &g.Name, &g.DisplayName, &g.Kind, &g.Type,
			&g.IsRequired, &g.AllowMultiple, &g.MinSelect, &g.MaxSelect, &g.SortOrder); err != nil {
			return nil, err
		}
		g.Options = []catalogapi.ModifierOption{}
		g.ProductIDs = []string{}
		byID[g.ID] = len(groups)
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(groups) == 0 {
		return groups, nil
	}

	optRows, err := s.pool.Query(ctx, `
		SELECT modifier_group_id, id, name, price_delta_cents, is_pre_selected, image_url, colour_hex,
		       is_gluten_free, allergen_flags, sort_order, is_active
		FROM modifier_options ORDER BY sort_order`)
	if err != nil {
		return nil, err
	}
	defer optRows.Close()
	for optRows.Next() {
		var groupID string
		var o catalogapi.ModifierOption
		if err := optRows.Scan(&groupID, &o.ID, &o.Name, &o.PriceDeltaCents, &o.IsPreSelected,
			&o.ImageURL, &o.ColourHex, &o.IsGlutenFree, &o.AllergenFlags, &o.SortOrder, &o.IsActive); err != nil {
			return nil, err
		}
		if idx, ok := byID[groupID]; ok {
			o.GroupID = groupID
			groups[idx].Options = append(groups[idx].Options, o)
		}
	}
	if err := optRows.Err(); err != nil {
		return nil, err
	}

	ruleRows, err := s.pool.Query(ctx, `
		SELECT bmr.id, bmr.branch_id, COALESCE(b.name, ''), bmr.modifier_group_id, bmr.modifier_option_id, bmr.price_override_cents,
		       bmr.available_from, bmr.available_until, bmr.reason, bmr.updated_by, bmr.updated_at
		FROM branch_modifier_rules bmr
		LEFT JOIN branches b ON b.id = bmr.branch_id
		WHERE bmr.price_override_cents IS NOT NULL
		  AND bmr.price_override_cents != 0`)
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

	linkRows, err := s.pool.Query(ctx, `SELECT modifier_group_id, product_id FROM product_modifier_groups`)
	if err != nil {
		return nil, err
	}
	defer linkRows.Close()
	for linkRows.Next() {
		var groupID, productID string
		if err := linkRows.Scan(&groupID, &productID); err != nil {
			return nil, err
		}
		if idx, ok := byID[groupID]; ok {
			groups[idx].ProductIDs = append(groups[idx].ProductIDs, productID)
		}
	}
	return groups, linkRows.Err()
}

// SaveModifierGroup upserts a group and replaces its options wholesale.
//
// Replace rather than diff: an option carries no meaning beyond its group, and
// orders snapshot the option values they captured rather than referencing
// them, so rewriting the set cannot rewrite history.
// ModifierGroupIDsForBranches returns the modifier groups used by at least one
// product sold at any of the given branches: an enabled branch_product_rules
// row, in a category the branch has enabled. Groups reach a product either
// through the product_modifier_groups library link or the older
// product-owned modifier_groups.product_id.
func (s *Repo) ModifierGroupIDsForBranches(ctx context.Context, branchIDs []string) (map[string]bool, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT link.group_id
		  FROM (SELECT modifier_group_id AS group_id, product_id FROM product_modifier_groups
		        UNION
		        SELECT id, product_id FROM modifier_groups WHERE product_id IS NOT NULL) link
		  JOIN products p ON p.id = link.product_id AND p.deleted_at IS NULL
		  JOIN branch_product_rules bpr
		    ON bpr.product_id = p.id AND bpr.branch_enabled AND bpr.branch_id::text = ANY($1)
		  JOIN branch_category_rules bcr
		    ON bcr.branch_id = bpr.branch_id AND bcr.category_id = p.category_id AND bcr.branch_enabled`,
		branchIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// ListProductModifierGroupIDs is the read side, used to answer "what does this
// product carry" without loading every group in the library.
func (s *Repo) ListProductModifierGroupIDs(ctx context.Context, productID string) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT modifier_group_id FROM product_modifier_groups WHERE product_id = $1 ORDER BY sort_order`,
		productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// GroupKinds reports the `kind` of each named modifier group.
//
// The console sends one flat list of group ids for a product's Variations and
// Add-Ons panels; the kind is what tells the two apart, and it lives on the
// group rather than in the request.
func (s *Repo) GroupKinds(ctx context.Context, groupIDs []string) (map[string]string, error) {
	out := map[string]string{}
	if len(groupIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id::text, COALESCE(kind, '') FROM modifier_groups WHERE id = ANY($1)`, groupIDs)
	if err != nil {
		return nil, fmt.Errorf("read modifier kinds: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, kind string
		if err := rows.Scan(&id, &kind); err != nil {
			return nil, err
		}
		out[id] = kind
	}
	return out, rows.Err()
}

// GroupOptions returns one group's options, in their own order.
func (s *Repo) GroupOptions(ctx context.Context, groupID string) ([]catalogapi.ModifierOption, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, name, price_delta_cents, sort_order, is_active
		  FROM modifier_options
		 WHERE modifier_group_id = $1
		 ORDER BY sort_order, name`, groupID)
	if err != nil {
		return nil, fmt.Errorf("read modifier options: %w", err)
	}
	defer rows.Close()

	var out []catalogapi.ModifierOption
	for rows.Next() {
		var o catalogapi.ModifierOption
		if err := rows.Scan(&o.ID, &o.Name, &o.PriceDeltaCents, &o.SortOrder, &o.IsActive); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
