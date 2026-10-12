// Package staff stores staff users and their role assignments.
package staff

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo reads and writes staff user rows.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }
