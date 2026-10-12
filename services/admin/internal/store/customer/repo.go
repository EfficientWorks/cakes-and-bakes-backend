// Package customer reads customers for the admin console, and stores staff notes and tags on them.
package customer

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo reads and writes customer rows.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }
