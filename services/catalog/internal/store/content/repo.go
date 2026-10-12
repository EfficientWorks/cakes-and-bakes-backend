// Package content stores the editable site content blocks (SF-35, SA-12).
package content

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo reads and writes site content rows.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }
