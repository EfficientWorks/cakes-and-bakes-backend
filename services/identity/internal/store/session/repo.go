// Package session stores refresh tokens: the sessions behind every signed-in customer, staff member, till and kitchen screen.
package session

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo reads and writes refresh token rows.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }
