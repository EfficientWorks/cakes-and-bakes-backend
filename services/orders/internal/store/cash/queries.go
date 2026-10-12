package cash

const cashDaySelect = `
	SELECT branch_id, to_char(business_date, 'YYYY-MM-DD'),
	       starting_cash_cents, carry_forward,
	       closed_at, counted_cash_cents, expected_cash_cents, shortage_cents, note
	  FROM pos_cash_days`
