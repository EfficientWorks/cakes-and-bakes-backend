package coupon

import (
	"errors"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/catalog/internal/store/shared"
)

func scanCoupon(row pgx.Row) (*catalogapi.Coupon, error) {
	var c catalogapi.Coupon
	err := row.Scan(&c.ID, &c.Code, &c.Name, &c.Type, &c.DiscountValue, &c.Status, &c.AppliesTo,
		&c.TargetIDs, &c.UsageLimit, &c.RedeemedCount, &c.HasActiveDates, &c.StartsAt, &c.EndsAt,
		&c.HasMinimumOrder, &c.MinimumOrderCents, &c.BranchID, &c.ShowOnCheckout, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.ErrNotFound
	}
	return &c, err
}
