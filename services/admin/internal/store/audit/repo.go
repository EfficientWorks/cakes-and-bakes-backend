// Package audit searches the audit log.
package audit

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo reads and writes audit log rows.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }
