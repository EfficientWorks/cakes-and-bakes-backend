// Package returns stores returns made against an order, priced per item.
package returns

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo reads and writes order return rows.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }
