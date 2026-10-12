// Package printtemplate stores the receipt, KOT and cancellation print templates.
package printtemplate

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo reads and writes print template rows.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }
