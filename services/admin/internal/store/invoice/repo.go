// Package invoice stores invoices.
//
// The party blocks are joined at read time rather than copied onto the row:
// an outlet's address changing should show on its invoices, because it is
// the same outlet. The money and the line text are copied, because those are
// what was billed and must not move.
package invoice

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo reads and writes invoice rows.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }
