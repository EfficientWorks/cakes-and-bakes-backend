// Package network reads the network-wide dashboard, trends and branch comparisons.
package network

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo reads and writes network report rows.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }
