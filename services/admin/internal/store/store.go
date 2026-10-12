// Package store is the admin service's database layer.
//
// Each folder holds one topic: its Repo (repo.go, with what the topic is
// for), and its queries, reads, writes and row scanning in their own files.
// Store gathers the topic repos into the one value the domain layer holds,
// so callers write s.store.X whichever folder the query lives in. Errors and
// helpers shared across topics live in store/shared.
package store

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"bakehouse/services/admin/internal/store/audit"
	"bakehouse/services/admin/internal/store/branchreport"
	"bakehouse/services/admin/internal/store/customer"
	"bakehouse/services/admin/internal/store/invoice"
	"bakehouse/services/admin/internal/store/network"
	"bakehouse/services/admin/internal/store/shared"
)

// One alias per topic: every package names its type Repo, and embedding
// needs distinct field names.
type (
	auditRepo        = audit.Repo
	branchreportRepo = branchreport.Repo
	customerRepo     = customer.Repo
	invoiceRepo      = invoice.Repo
	networkRepo      = network.Repo
)

// Store is every admin repo behind one value.
type Store struct {
	*auditRepo
	*branchreportRepo
	*customerRepo
	*invoiceRepo
	*networkRepo
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{
		auditRepo:        audit.New(pool),
		branchreportRepo: branchreport.New(pool),
		customerRepo:     customer.New(pool),
		invoiceRepo:      invoice.New(pool),
		networkRepo:      network.New(pool),
	}
}

// Names the domain layer uses as store.X.
var (
	ErrNotFound = shared.ErrNotFound
)
