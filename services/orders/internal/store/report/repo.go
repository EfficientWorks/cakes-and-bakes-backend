// Package report runs the sales, product and return reports for the POS and admin.
//
// Every query counts in the store's own timezone (see the cash package).
package report

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo reads and writes sales report rows.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }
