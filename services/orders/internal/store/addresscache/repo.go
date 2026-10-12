// Package addresscache caches address-search results so repeated lookups do not hit the geocoder.
package addresscache

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo reads and writes address search cache rows.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }
