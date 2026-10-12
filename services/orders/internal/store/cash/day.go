package cash

import (
	"context"

	"github.com/jackc/pgx/v5"

	"bakehouse/pkg/timex"
	"bakehouse/services/orders/internal/store/shared"
	"bakehouse/services/orders/ordersapi"
)

// GetCashDay reads the stored row for one day, if there is one.
func (s *Repo) GetCashDay(ctx context.Context, branchID, date string) (*ordersapi.CashDay, error) {
	return scanCashDay(s.pool.QueryRow(ctx,
		cashDaySelect+` WHERE branch_id = $1 AND business_date = $2::date`, branchID, date))
}

/*
 * StandingFloat finds the most recent "apply the same amount daily" setting
 * on or before a date.
 *
 * This is what makes the tick box mean anything on a day nobody has opened:
 * rather than writing a row for every future date the moment somebody ticks
 * it, the amount is looked back for when a day is read. A shop that closes for
 * a fortnight comes back to the float it left, with no rows invented in
 * between.
 */
func (s *Repo) StandingFloat(ctx context.Context, branchID, date string) (int64, bool, error) {
	var cents int64
	err := s.pool.QueryRow(ctx, `
		SELECT starting_cash_cents
		  FROM pos_cash_days
		 WHERE branch_id = $1 AND business_date <= $2::date AND carry_forward
		 ORDER BY business_date DESC
		 LIMIT 1`, branchID, date).Scan(&cents)
	if err == pgx.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return cents, true, nil
}

// UpsertStartingCash sets the opening float for a day, creating the day if
// this is the first thing recorded against it.
func (s *Repo) UpsertStartingCash(ctx context.Context, in ordersapi.StartingCashInput) (*ordersapi.CashDay, error) {
	q := `
		INSERT INTO pos_cash_days (branch_id, business_date, starting_cash_cents, carry_forward)
		VALUES ($1, $2::date, $3, $4)
		ON CONFLICT (branch_id, business_date) DO UPDATE
		   SET starting_cash_cents = EXCLUDED.starting_cash_cents,
		       carry_forward       = EXCLUDED.carry_forward,
		       updated_at          = NOW()
		RETURNING branch_id, to_char(business_date, 'YYYY-MM-DD'),
		          starting_cash_cents, carry_forward,
		          closed_at, counted_cash_cents, expected_cash_cents, shortage_cents, note`
	return scanCashDay(s.pool.QueryRow(ctx, q,
		in.BranchID, in.BusinessDate, in.AmountCents, in.CarryForward))
}

/*
 * CloseDay records the counted drawer.
 *
 * Expected and shortage are stored rather than recomputed on every read. What
 * the drawer was expected to hold is a statement about the day as it was
 * counted; a refund keyed in the next morning must not quietly rewrite last
 * night's shortage into something nobody signed off.
 *
 * Re-closing the same day overwrites it, which is deliberate — a miscount
 * found ten minutes later should be correctable without inventing a second
 * record of the same day.
 */
func (s *Repo) CloseDay(ctx context.Context, in ordersapi.DayCloseInput, closedBy string, expected, shortage int64) (*ordersapi.CashDay, error) {
	q := `
		INSERT INTO pos_cash_days (
		    branch_id, business_date,
		    counted_cash_cents, expected_cash_cents, shortage_cents,
		    note, closed_at, closed_by)
		VALUES ($1,$2::date,$3,$4,$5,$6,COALESCE($8::timestamptz, NOW()),$7)
		ON CONFLICT (branch_id, business_date) DO UPDATE
		   SET counted_cash_cents  = EXCLUDED.counted_cash_cents,
		       expected_cash_cents = EXCLUDED.expected_cash_cents,
		       shortage_cents      = EXCLUDED.shortage_cents,
		       note                = EXCLUDED.note,
		       closed_at           = EXCLUDED.closed_at,
		       closed_by           = EXCLUDED.closed_by,
		       updated_at          = NOW()
		RETURNING branch_id, to_char(business_date, 'YYYY-MM-DD'),
		          starting_cash_cents, carry_forward,
		          closed_at, counted_cash_cents, expected_cash_cents, shortage_cents, note`
	return scanCashDay(s.pool.QueryRow(ctx, q,
		in.BranchID, in.BusinessDate,
		in.CountedCashCents, expected, shortage, in.Note, shared.NilIfEmptyStr(closedBy),
		timex.EventTimeFrom(ctx)))
}
