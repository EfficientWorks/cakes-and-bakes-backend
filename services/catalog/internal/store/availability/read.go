package availability

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/catalog/catalogapi"
)

func (s *Repo) GetBranchCategoryRule(ctx context.Context, branchID, categoryID string) (*catalogapi.BranchCategoryRule, error) {
	var r catalogapi.BranchCategoryRule
	err := s.pool.QueryRow(ctx, `
		SELECT id, branch_id, category_id, branch_enabled, 
		       available_from, available_until, reason, updated_by, updated_at
		FROM branch_category_rules WHERE branch_id = $1 AND category_id = $2`, branchID, categoryID).
		Scan(&r.ID, &r.BranchID, &r.CategoryID, &r.BranchEnabled,
			&r.AvailableFrom, &r.AvailableUntil, &r.Reason, &r.UpdatedBy, &r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		// No rule row yet = defaults: disabled/unavailable for this branch.
		return &catalogapi.BranchCategoryRule{BranchID: branchID, CategoryID: categoryID, BranchEnabled: false}, nil
	}

	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *Repo) ListBranchCategoryRules(ctx context.Context, branchID string) (map[string]catalogapi.BranchCategoryRule, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, branch_id, category_id, branch_enabled, 
		       available_from, available_until, reason, updated_by, updated_at
		FROM branch_category_rules WHERE branch_id = $1`, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]catalogapi.BranchCategoryRule{}
	for rows.Next() {
		var r catalogapi.BranchCategoryRule
		if err := rows.Scan(&r.ID, &r.BranchID, &r.CategoryID, &r.BranchEnabled,
			&r.AvailableFrom, &r.AvailableUntil, &r.Reason, &r.UpdatedBy, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out[r.CategoryID] = r
	}
	return out, rows.Err()
}

func (s *Repo) GetBranchProductRule(ctx context.Context, branchID, productID string) (*catalogapi.BranchProductRule, error) {
	var r catalogapi.BranchProductRule
	var days []string
	var overridesRaw []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, branch_id, product_id, branch_enabled, available_days, date_overrides,
		       price_override_cents, available_from, available_until, reason, updated_by, updated_at
		FROM branch_product_rules WHERE branch_id = $1 AND product_id = $2`, branchID, productID).
		Scan(&r.ID, &r.BranchID, &r.ProductID, &r.BranchEnabled, &days, &overridesRaw,
			&r.PriceOverrideCents, &r.AvailableFrom, &r.AvailableUntil, &r.Reason, &r.UpdatedBy, &r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		// No rule row yet = defaults: disabled/unavailable for this branch.
		return &catalogapi.BranchProductRule{
			BranchID:      branchID,
			ProductID:     productID,
			BranchEnabled: false,
			AvailableDays: catalogapi.DefaultAvailableDays,
		}, nil
	}

	if err != nil {
		return nil, err
	}
	r.AvailableDays = days
	_ = json.Unmarshal(overridesRaw, &r.DateOverrides)
	return &r, nil
}

func (s *Repo) ListBranchProductRules(ctx context.Context, branchID string) (map[string]catalogapi.BranchProductRule, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, branch_id, product_id, branch_enabled, available_days, date_overrides,
		       price_override_cents, available_from, available_until, reason, updated_by, updated_at
		FROM branch_product_rules WHERE branch_id = $1`, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]catalogapi.BranchProductRule{}
	for rows.Next() {
		var r catalogapi.BranchProductRule
		var overridesRaw []byte
		if err := rows.Scan(&r.ID, &r.BranchID, &r.ProductID, &r.BranchEnabled, &r.AvailableDays, &overridesRaw,
			&r.PriceOverrideCents, &r.AvailableFrom, &r.AvailableUntil, &r.Reason, &r.UpdatedBy, &r.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(overridesRaw, &r.DateOverrides)
		out[r.ProductID] = r
	}
	return out, rows.Err()
}

func (s *Repo) GetBranchProductRulesByProductIDs(ctx context.Context, branchID string, productIDs []string) (map[string]catalogapi.BranchProductRule, error) {
	out := make(map[string]catalogapi.BranchProductRule)
	if len(productIDs) == 0 || branchID == "" {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, branch_id, product_id, branch_enabled, available_days, date_overrides,
		       price_override_cents, available_from, available_until, reason, updated_by, updated_at
		FROM branch_product_rules
		WHERE branch_id = $1 AND product_id = ANY($2)`, branchID, productIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var r catalogapi.BranchProductRule
		var overridesRaw []byte
		if err := rows.Scan(&r.ID, &r.BranchID, &r.ProductID, &r.BranchEnabled, &r.AvailableDays, &overridesRaw,
			&r.PriceOverrideCents, &r.AvailableFrom, &r.AvailableUntil, &r.Reason, &r.UpdatedBy, &r.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(overridesRaw, &r.DateOverrides)
		out[r.ProductID] = r
	}
	return out, rows.Err()
}

func (s *Repo) GetBranchModifierRulesByOptionIDs(ctx context.Context, branchID string, optionIDs []string) (map[string]catalogapi.BranchModifierRule, error) {
	out := make(map[string]catalogapi.BranchModifierRule)
	if len(optionIDs) == 0 || branchID == "" {
		return out, nil
	}

	rows, err := s.pool.Query(ctx, `
		SELECT bmr.id, bmr.branch_id, COALESCE(b.name, ''), bmr.modifier_group_id, bmr.modifier_option_id, bmr.price_override_cents,
		       bmr.available_from, bmr.available_until, bmr.reason, bmr.updated_by, bmr.updated_at
		FROM branch_modifier_rules bmr
		LEFT JOIN branches b ON b.id = bmr.branch_id
		WHERE bmr.branch_id = $1 AND bmr.modifier_option_id = ANY($2)
		  AND bmr.price_override_cents IS NOT NULL
		  AND bmr.price_override_cents != 0`, branchID, optionIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var r catalogapi.BranchModifierRule
		if err := rows.Scan(&r.ID, &r.BranchID, &r.BranchName, &r.ModifierGroupID, &r.ModifierOptionID,
			&r.PriceOverrideCents, &r.AvailableFrom, &r.AvailableUntil, &r.Reason,
			&r.UpdatedBy, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out[r.ModifierOptionID] = r
	}
	return out, rows.Err()
}
