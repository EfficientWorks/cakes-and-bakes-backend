package modifier

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

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
	}

	existing, err := loadExistingOptions(ctx, tx, id)
	if err != nil {
		return "", err
	}
	// Store prices are written only from this form, so the request's list is
	// the whole truth: clear the group's and write back what was sent.
	if _, err := tx.Exec(ctx, `DELETE FROM branch_modifier_rules WHERE modifier_group_id = $1`, id); err != nil {
		return "", err
	}

	kept := make([]string, 0, len(g.Options))
	for i, o := range g.Options {
		optionID := existing.claim(o.ID, o.Name)
		if optionID != "" {
			if _, err := tx.Exec(ctx, `
				UPDATE modifier_options
				   SET name=$2, price_delta_cents=$3, is_pre_selected=$4, image_url=$5, colour_hex=$6,
				       is_gluten_free=$7, allergen_flags=$8, sort_order=$9, is_active=$10
				 WHERE id=$1`,
				optionID, o.Name, o.PriceDeltaCents, o.IsPreSelected, o.ImageURL, o.ColourHex,
				o.IsGlutenFree, shared.OrEmptyStrings(o.AllergenFlags), i+1, true); err != nil {
				return "", fmt.Errorf("update modifier option: %w", err)
			}
		} else {
			err := tx.QueryRow(ctx, `
				INSERT INTO modifier_options (modifier_group_id, name, price_delta_cents, is_pre_selected,
					image_url, colour_hex, is_gluten_free, allergen_flags, sort_order, is_active)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`,
				id, o.Name, o.PriceDeltaCents, o.IsPreSelected, o.ImageURL, o.ColourHex,
				o.IsGlutenFree, shared.OrEmptyStrings(o.AllergenFlags), i+1, true).Scan(&optionID)
			if err != nil {
				return "", fmt.Errorf("insert modifier option: %w", err)
			}
		}
		kept = append(kept, optionID)

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

	// Options left out of the request were removed in the form.
	if _, err := tx.Exec(ctx, `
		DELETE FROM modifier_options WHERE modifier_group_id = $1 AND NOT (id = ANY($2::uuid[]))`,
		id, kept); err != nil {
		return "", fmt.Errorf("delete removed modifier options: %w", err)
	}

	return id, tx.Commit(ctx)
}

// existingOptions are a group's saved options, so a save can keep their ids.
//
// Option ids are referenced by product combination rows ("8 Inch | Vanilla |
// Butter Cream") and by basket lines. Re-creating every option on each save
// gave them new ids and broke every row the moment staff added one more
// filling. An option is matched by the id the console sent, or, for a caller
// that sends none, by its name; each saved option is claimed at most once.
type existingOptions struct {
	ids     map[string]bool
	byName  map[string][]string
	claimed map[string]bool
}

func loadExistingOptions(ctx context.Context, tx pgx.Tx, groupID string) (*existingOptions, error) {
	e := &existingOptions{ids: map[string]bool{}, byName: map[string][]string{}, claimed: map[string]bool{}}
	rows, err := tx.Query(ctx, `
		SELECT id::text, name FROM modifier_options WHERE modifier_group_id = $1 ORDER BY sort_order, id`, groupID)
	if err != nil {
		return nil, fmt.Errorf("load modifier options: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		e.ids[id] = true
		key := optionNameKey(name)
		e.byName[key] = append(e.byName[key], id)
	}
	return e, rows.Err()
}

// claim returns the saved option this request option is, or "" for a new one.
func (e *existingOptions) claim(id, name string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if e.ids[id] && !e.claimed[id] {
		e.claimed[id] = true
		return id
	}
	for _, candidate := range e.byName[optionNameKey(name)] {
		if !e.claimed[candidate] {
			e.claimed[candidate] = true
			return candidate
		}
	}
	return ""
}

func optionNameKey(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

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
