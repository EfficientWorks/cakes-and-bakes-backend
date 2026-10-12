// Package customer stores storefront and POS customers: profile, verification codes and password reset.
package customer

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo reads and writes customer rows.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }
