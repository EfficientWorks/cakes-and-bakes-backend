// Package customer stores the contact details of the customer a till order is rung up against.
package customer

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo reads and writes POS customer rows.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }
