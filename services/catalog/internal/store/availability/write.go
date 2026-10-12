package availability

import (
	"context"
	"encoding/json"

	"bakehouse/pkg/strx"
	"bakehouse/services/catalog/catalogapi"
)

// UpsertBranchProductRulePatch applies a partial update, creating the row
// (from defaults) if it does not exist yet.
func (s *Repo) UpsertBranchProductRulePatch(ctx context.Context, branchID, productID string, patch func(*catalogapi.BranchProductRule), updatedBy, reason string) (*catalogapi.BranchProductRule, error) {
	current, err := s.GetBranchProductRule(ctx, branchID, productID)
	if err != nil {
		return nil, err
	}
	patch(current)
	if reason != "" {
		current.Reason = &reason
	}

	overridesJSON, _ := json.Marshal(current.DateOverrides)
	const q = `
		INSERT INTO branch_product_rules (branch_id, product_id, branch_enabled, available_days, date_overrides,
			price_override_cents, available_from, available_until, reason, updated_by, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NOW())
		ON CONFLICT (branch_id, product_id) DO UPDATE SET
			branch_enabled = $3, available_days = $4, date_overrides = $5, price_override_cents = $6,
			available_from = $7, available_until = $8, reason = $9, updated_by = $10, updated_at = NOW()
		RETURNING id, updated_at`
	err = s.pool.QueryRow(ctx, q, branchID, productID, current.BranchEnabled, current.AvailableDays, overridesJSON,
		current.PriceOverrideCents, current.AvailableFrom, current.AvailableUntil, current.Reason, strx.NilIfEmpty(updatedBy)).
		Scan(&current.ID, &current.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return current, nil
}

func (s *Repo) BulkDisable(ctx context.Context, branchID string, productIDs []string, reason, updatedBy string) (int, error) {
	count := 0
	for _, pid := range productIDs {
		_, err := s.UpsertBranchProductRulePatch(ctx, branchID, pid, func(r *catalogapi.BranchProductRule) {
			r.BranchEnabled = false
		}, updatedBy, reason)
		if err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}
