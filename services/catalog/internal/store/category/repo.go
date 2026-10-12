// Package category stores product categories: the storefront's reads and the admin's create, edit and hide.
package category

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo reads and writes category rows.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }
