// Package business stores the businesses registered on the platform.
package business

import "github.com/jackc/pgx/v5/pgxpool"

// Repo reads and writes business rows.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }
