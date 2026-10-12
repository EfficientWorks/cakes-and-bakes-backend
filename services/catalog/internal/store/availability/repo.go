// Package availability stores the per-branch availability cascade: category and product rules a store sets on top of the master catalogue.
package availability

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo reads and writes branch availability rule rows.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }
