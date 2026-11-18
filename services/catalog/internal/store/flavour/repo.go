// Package flavour stores the home page's flavour cards and which products
// each one lists. Every query is limited to the request's business
// (tenant.BusinessParam, always $1 in flavourScoped).
package flavour

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo reads and writes flavour rows.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }
