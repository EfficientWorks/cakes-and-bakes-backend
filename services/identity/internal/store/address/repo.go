// Package address stores a customer's saved delivery addresses.
package address

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo reads and writes customer address rows.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }
