// Package tag stores seasonal tags, which products carry them, and the dates they restrict.
package tag

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo reads and writes seasonal tag rows.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }
