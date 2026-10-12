// Package printer stores the POS and KDS printers at each location.
package printer

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo reads and writes printer rows.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }
