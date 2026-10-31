// Package upload records the photos customers upload for their cakes, so an
// order can prove a photo URL is one this server stored rather than any link.
package upload

import "github.com/jackc/pgx/v5/pgxpool"

// Repo reads and writes customer_photo_uploads.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }
