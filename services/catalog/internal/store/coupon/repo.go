// Package coupon stores coupons and their redemptions.
package coupon

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo reads and writes coupon rows.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }
