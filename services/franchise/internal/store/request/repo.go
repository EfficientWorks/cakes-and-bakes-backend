// Package request stores requests to open, change or close a branch, and their approval.
package request

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo reads and writes branch request rows.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }
