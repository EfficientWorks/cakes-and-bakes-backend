// Package branchreport reads one branch's dashboard and performance reports.
//
// Every query in the admin store is read-only against tables owned by other
// services (orders, refunds, customers, branches, audit_log).
package branchreport

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo reads and writes branch report rows.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }
