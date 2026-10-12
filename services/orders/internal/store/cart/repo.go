// Package cart stores storefront carts: the guest or customer basket, its lines and its fulfilment choice.
package cart

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo reads and writes cart rows.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }
