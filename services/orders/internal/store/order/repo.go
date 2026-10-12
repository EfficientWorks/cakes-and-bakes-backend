// Package order stores orders: creating them, reading them with their lines, bills,
// returns and timeline, listing them for the POS tabs and admin, and the
// updates made to them afterwards.
//
// Which POS tab an order sits in (queries.go): the tabs are complementary,
// so an order is in exactly one place. History is paid (and, for a website
// order, also handed over by the kitchen), or cancelled / returned /
// refunded. Scheduled is not history and due on a later day at the store.
// Today is not history and due today or earlier. "Today" is the store's own
// date, so a scheduled order moves to today's tab on its day without
// anything having to change it.
package order

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"bakehouse/services/orders/internal/store/bill"
	"bakehouse/services/orders/internal/store/returns"
)

// Repo reads and writes order rows.
type Repo struct {
	pool    *pgxpool.Pool
	bills   *bill.Repo
	returns *returns.Repo
}

func New(pool *pgxpool.Pool, bills *bill.Repo, returns *returns.Repo) *Repo {
	return &Repo{pool: pool, bills: bills, returns: returns}
}
