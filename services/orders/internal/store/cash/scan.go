package cash

import (
	"github.com/jackc/pgx/v5"

	"bakehouse/services/orders/ordersapi"
)

func scanCashDay(row pgx.Row) (*ordersapi.CashDay, error) {
	var d ordersapi.CashDay
	if err := row.Scan(&d.BranchID, &d.BusinessDate,
		&d.StartingCashCents, &d.CarryForward,
		&d.ClosedAt, &d.CountedCashCents, &d.ExpectedCashCents, &d.ShortageCents, &d.Note); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ordersapi.ErrNotFound
		}
		return nil, err
	}
	return &d, nil
}
