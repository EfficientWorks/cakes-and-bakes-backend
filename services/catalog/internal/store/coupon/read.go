package coupon

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/catalog/catalogapi"
)

func (s *Repo) ListCoupons(ctx context.Context) ([]catalogapi.Coupon, error) {
	rows, err := s.pool.Query(ctx, couponSelect+` WHERE deleted_at IS NULL ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []catalogapi.Coupon{}
	for rows.Next() {
		c, err := scanCoupon(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (s *Repo) GetCoupon(ctx context.Context, id string) (*catalogapi.Coupon, error) {
	return scanCoupon(s.pool.QueryRow(ctx, couponSelect+` WHERE id = $1 AND deleted_at IS NULL`, id))
}

// GetCouponByCode finds a live coupon by the code a shopper typed.
//
// Compared case-insensitively on both sides: codes are printed on cards and
// pasted out of emails, and "freedel" not working when "FREEDEL" does is not a
// distinction worth making.
func (s *Repo) GetCouponByCode(ctx context.Context, code string) (*catalogapi.Coupon, error) {
	return scanCoupon(s.pool.QueryRow(ctx,
		couponSelect+` WHERE upper(code) = upper($1) AND deleted_at IS NULL`, code))
}

// ActiveNewCustomerCouponID returns the id of the live new-customer coupon,
// or "" when there is none. Used to refuse a second one before the unique
// index does so less politely.
func (s *Repo) ActiveNewCustomerCouponID(ctx context.Context) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `
		SELECT id::text FROM coupons
		 WHERE applies_to = 'new_customer' AND status = 'active' AND deleted_at IS NULL
		 LIMIT 1`).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return id, err
}

// ListStorefrontCoupons is the public list: active codes only, filtered.
func (s *Repo) ListStorefrontCoupons(ctx context.Context, f StorefrontCouponFilter) ([]catalogapi.Coupon, error) {
	q := couponSelect + `
		WHERE deleted_at IS NULL
		  AND status = 'active'
		  AND (usage_limit IS NULL OR redeemed_count < usage_limit)`
	args := []interface{}{}
	if f.BranchID != "" {
		args = append(args, f.BranchID)
		q += fmt.Sprintf(" AND (branch_id IS NULL OR branch_id = $%d::uuid)", len(args))
	}
	if f.ShowOnCheckout != nil {
		args = append(args, *f.ShowOnCheckout)
		q += fmt.Sprintf(" AND show_on_checkout = $%d", len(args))
	}
	if f.NewCustomer != nil {
		args = append(args, *f.NewCustomer)
		q += fmt.Sprintf(" AND (applies_to = 'new_customer') = $%d", len(args))
	}
	if f.Date != nil {
		args = append(args, *f.Date)
		// The end date is a whole day, the same as Coupon.Quote reads it.
		q += fmt.Sprintf(` AND (NOT has_active_dates
		    OR ((starts_at IS NULL OR starts_at <= $%d::timestamptz)
		        AND (ends_at IS NULL OR ends_at + INTERVAL '1 day' > $%d::timestamptz)))`, len(args), len(args))
	}
	q += " ORDER BY applies_to = 'new_customer' DESC, created_at DESC"

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []catalogapi.Coupon{}
	for rows.Next() {
		c, err := scanCoupon(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// CustomerIsNew reports whether a customer qualifies for a first-order code:
// signed in, with no order on record that was not cancelled. Orders are read
// directly — the same database, and a read of one column is not worth a
// cross-module contract.
func (s *Repo) CustomerIsNew(ctx context.Context, customerID, couponID string) (bool, error) {
	var orders int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM orders
		 WHERE customer_id = $1::uuid AND deleted_at IS NULL AND status <> 'cancelled'`,
		customerID).Scan(&orders)
	if err != nil {
		return false, fmt.Errorf("customer is new: %w", err)
	}
	return orders == 0, nil
}

// CategoryIDsForProducts returns the distinct categories a set of products
// belongs to. One query rather than one per line, because a basket of ten
// items should not be ten round trips.
func (s *Repo) CategoryIDsForProducts(ctx context.Context, productIDs []string) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT category_id::text FROM products WHERE id = ANY($1) AND deleted_at IS NULL`,
		productIDs)
	if err != nil {
		return nil, fmt.Errorf("categories for products: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
