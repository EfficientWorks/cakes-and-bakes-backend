// Package branch stores branches (stores): their settings, opening hours, delivery pricing,
// business id, closures and status.
package branch

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo reads and writes branch rows.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }
