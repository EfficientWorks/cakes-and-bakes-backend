package modifier

import (
	"context"
	"fmt"
	"strings"

	"bakehouse/pkg/tenant"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/catalog/internal/store/shared"
)

func (s *Repo) SaveModifierGroup(ctx context.Context, g catalogapi.ModifierGroup) (string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var maxSelect *int
	if g.MaxSelect > 0 {
		maxSelect = &g.MaxSelect
	}

	id := g.ID
	if id == "" {
		err = tx.QueryRow(ctx, `
			INSERT INTO modifier_groups (product_id, name, display_name, kind, type, is_required,
				allow_multiple, min_select, max_select, sort_order, business_id)
			VALUES (NULL,$1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`,
			g.Name, g.DisplayName, g.Kind, g.Type, g.IsRequired,
			g.AllowMultiple, g.MinSelect, maxSelect, g.SortOrder, tenant.BusinessParam(ctx)).Scan(&id)
		if err != nil {
			return "", fmt.Errorf("insert modifier group: %w", err)
		}
	} else {
		tag, err := tx.Exec(ctx, `
			UPDATE modifier_groups
			   SET name=$2, display_name=$3, kind=$4, type=$5, is_required=$6,
			       allow_multiple=$7, min_select=$8, max_select=$9, sort_order=$10
			 WHERE id=$1 AND business_id IS NOT DISTINCT FROM $11`,
			id, g.Name, g.DisplayName, g.Kind, g.Type, g.IsRequired,
			g.AllowMultiple, g.MinSelect, maxSelect, g.SortOrder, tenant.BusinessParam(ctx))
		if err != nil {
			return "", fmt.Errorf("update modifier group: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return "", shared.ErrNotFound
		}
		if _, err := tx.Exec(ctx, `DELETE FROM modifier_options WHERE modifier_group_id = $1`, id); err != nil {
			return "", err
		}
	}

	for i, o := range g.Options {
		var optionID string
		err := tx.QueryRow(ctx, `
			INSERT INTO modifier_options (modifier_group_id, name, price_delta_cents, is_pre_selected,
				image_url, colour_hex, is_gluten_free, allergen_flags, sort_order, is_active)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`,
			id, o.Name, o.PriceDeltaCents, o.IsPreSelected, o.ImageURL, o.ColourHex,
			o.IsGlutenFree, shared.OrEmptyStrings(o.AllergenFlags), i+1, true).Scan(&optionID)
		if err != nil {
			return "", fmt.Errorf("insert modifier option: %w", err)
		}

		for _, sp := range o.StorePrices {
			branchID := strings.TrimSpace(sp.BranchID)
			if branchID == "" {
				continue
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO branch_modifier_rules (branch_id, modifier_group_id, modifier_option_id, price_override_cents, updated_at)
				VALUES ($1, $2, $3, $4, NOW())
				ON CONFLICT (branch_id, modifier_group_id, modifier_option_id)
				DO UPDATE SET price_override_cents = EXCLUDED.price_override_cents, updated_at = NOW()`,
				branchID, id, optionID, sp.PriceOverrideCents); err != nil {
				return "", fmt.Errorf("insert branch modifier rule: %w", err)
			}
		}
	}

	return id, tx.Commit(ctx)
}

func (s *Repo) DeleteModifierGroup(ctx context.Context, id string) error {
	// Options and the product links both cascade, so one delete is enough.
	tag, err := s.pool.Exec(ctx, `DELETE FROM modifier_groups WHERE id = $1 AND business_id IS NOT DISTINCT FROM $2`, id, tenant.BusinessParam(ctx))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}

// ApplyModifierToProducts replaces the set of products a group is applied to.
//
// Delete-then-insert inside one transaction: the dialog sends the complete
// list it wants, and computing a diff would leave a window where a product is
// linked to neither the old set nor the new one.
func (s *Repo) ApplyModifierToProducts(ctx context.Context, groupID string, productIDs []string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM product_modifier_groups WHERE modifier_group_id = $1`, groupID); err != nil {
		return err
	}
	for i, productID := range productIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO product_modifier_groups (product_id, modifier_group_id, sort_order)
			VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, productID, groupID, i); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// SetProductModifierGroups replaces which library groups a product carries.
//
// The mirror of ApplyModifierToProducts: that one is "this modifier, these
// products", this is "this product, these modifiers". Both exist because the
// console has a screen for each direction — the modifier list's "Apply To
// Items", and the product form's Variations and Add-Ons panels — and making
// either reuse the other would replace the wrong side of the relationship.
func (s *Repo) SetProductModifierGroups(ctx context.Context, productID string, groupIDs []string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM product_modifier_groups WHERE product_id = $1`, productID); err != nil {
		return err
	}
	for i, groupID := range groupIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO product_modifier_groups (product_id, modifier_group_id, sort_order)
			VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, productID, groupID, i); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
