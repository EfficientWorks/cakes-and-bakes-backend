// Package modifier stores the reusable modifier library (sizes, flavours, add-ons) and which products use it.
package modifier

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo reads and writes modifier group rows.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }
