package coupon

import (
	"context"
	"fmt"
	"strings"

	"bakehouse/pkg/tenant"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/catalog/internal/store/shared"
)

func (s *Repo) CreateCoupon(ctx context.Context, c catalogapi.Coupon, createdBy string) (*catalogapi.Coupon, error) {
	q := `
		INSERT INTO coupons (code, name, type, discount_value, status, applies_to, target_ids,
			usage_limit, has_active_dates, starts_at, ends_at, has_minimum_order, minimum_order_cents,
			branch_id, created_by, show_on_checkout, business_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		RETURNING ` + couponColumns
	return scanCoupon(s.pool.QueryRow(ctx, q,
		strings.ToUpper(c.Code), c.Name, c.Type, c.DiscountValue, c.Status, c.AppliesTo, shared.OrEmptyStrings(c.TargetIDs),
		c.UsageLimit, c.HasActiveDates, c.StartsAt, c.EndsAt, c.HasMinimumOrder, c.MinimumOrderCents,
		c.BranchID, shared.OrNilUUID(createdBy), c.ShowOnCheckout, tenant.BusinessParam(ctx)))
}

func (s *Repo) UpdateCoupon(ctx context.Context, id string, c catalogapi.Coupon) (*catalogapi.Coupon, error) {
	q := `
		UPDATE coupons
		   SET code=$2, name=$3, type=$4, discount_value=$5, status=$6, applies_to=$7, target_ids=$8,
		       usage_limit=$9, has_active_dates=$10, starts_at=$11, ends_at=$12,
		       has_minimum_order=$13, minimum_order_cents=$14, branch_id=$15,
		       show_on_checkout=$16, updated_at=NOW()
		 WHERE id=$1 AND deleted_at IS NULL AND business_id IS NOT DISTINCT FROM $17
		RETURNING ` + couponColumns
	return scanCoupon(s.pool.QueryRow(ctx, q,
		id, strings.ToUpper(c.Code), c.Name, c.Type, c.DiscountValue, c.Status, c.AppliesTo, shared.OrEmptyStrings(c.TargetIDs),
		c.UsageLimit, c.HasActiveDates, c.StartsAt, c.EndsAt, c.HasMinimumOrder, c.MinimumOrderCents, c.BranchID,
		c.ShowOnCheckout, tenant.BusinessParam(ctx)))
}

func (s *Repo) SetCouponStatus(ctx context.Context, id, status string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE coupons SET status = $2, updated_at = NOW()
		  WHERE id = $1 AND deleted_at IS NULL AND business_id IS NOT DISTINCT FROM $3`, id, status, tenant.BusinessParam(ctx))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}

// DeleteCoupon is a soft delete. Redemptions reference the coupon and an order
// that used it keeps its discount; removing the row would break both.
func (s *Repo) DeleteCoupon(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE coupons SET deleted_at = NOW()
		  WHERE id = $1 AND deleted_at IS NULL AND business_id IS NOT DISTINCT FROM $2`, id, tenant.BusinessParam(ctx))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}

// RecordCouponRedemption records one use, against the order that used it.
//
// Two writes in one transaction: the ledger row (which is what a per-customer
// rule reads back) and the counter (which is what the usage limit reads).
// The counter's WHERE clause is the limit's guard — two shoppers checking out
// with the last use of a coupon at the same moment would otherwise both
// succeed — and zero rows affected means someone else got there first. The
// ledger's UNIQUE(order_id) makes a replayed order a no-op rather than a
// second use.
func (s *Repo) RecordCouponRedemption(ctx context.Context, couponID, orderID string, customerID *string, amountCents int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		INSERT INTO coupon_redemptions (coupon_id, order_id, customer_id, amount_cents)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (order_id) DO NOTHING`, couponID, orderID, customerID, amountCents)
	if err != nil {
		return fmt.Errorf("record coupon redemption: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Already counted for this order.
		return tx.Commit(ctx)
	}

	tag, err = tx.Exec(ctx, `
		UPDATE coupons
		   SET redeemed_count = redeemed_count + 1, updated_at = NOW()
		 WHERE id = $1
		   AND deleted_at IS NULL
		   AND (usage_limit IS NULL OR redeemed_count < usage_limit)`, couponID)
	if err != nil {
		return fmt.Errorf("increment coupon redemption: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return catalogapi.ErrCouponUsedUp
	}
	return tx.Commit(ctx)
}
