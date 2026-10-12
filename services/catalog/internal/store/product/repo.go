// Package product stores products with their images, variants, modifier groups and tags, and searches them.
package product

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"bakehouse/services/catalog/internal/store/tag"
)

// Repo reads and writes product rows.
type Repo struct {
	pool *pgxpool.Pool
	tags *tag.Repo
}

func New(pool *pgxpool.Pool, tags *tag.Repo) *Repo { return &Repo{pool: pool, tags: tags} }
