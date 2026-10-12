// Package recipient stores the gift recipients a customer has saved on their account.
package recipient

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo reads and writes customer recipient rows.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }
