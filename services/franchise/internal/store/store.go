// Package store is the franchise service's database layer.
//
// Each folder holds one topic: its Repo (repo.go, with what the topic is
// for), and its queries, reads, writes and row scanning in their own files.
// Store gathers the topic repos into the one value the domain layer holds,
// so callers write s.store.X whichever folder the query lives in. Errors and
// helpers shared across topics live in store/shared.
package store

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"bakehouse/services/franchise/internal/store/branch"
	"bakehouse/services/franchise/internal/store/kdssettings"
	"bakehouse/services/franchise/internal/store/kitchen"
	"bakehouse/services/franchise/internal/store/printer"
	"bakehouse/services/franchise/internal/store/printtemplate"
	"bakehouse/services/franchise/internal/store/request"
	"bakehouse/services/franchise/internal/store/shared"
)

// One alias per topic: every package names its type Repo, and embedding
// needs distinct field names.
type (
	branchRepo        = branch.Repo
	kdssettingsRepo   = kdssettings.Repo
	kitchenRepo       = kitchen.Repo
	printerRepo       = printer.Repo
	printtemplateRepo = printtemplate.Repo
	requestRepo       = request.Repo
)

// Store is every franchise repo behind one value.
type Store struct {
	*branchRepo
	*kdssettingsRepo
	*kitchenRepo
	*printerRepo
	*printtemplateRepo
	*requestRepo
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{
		branchRepo:        branch.New(pool),
		kdssettingsRepo:   kdssettings.New(pool),
		kitchenRepo:       kitchen.New(pool),
		printerRepo:       printer.New(pool),
		printtemplateRepo: printtemplate.New(pool),
		requestRepo:       request.New(pool),
	}
}

// Names the domain layer uses as store.X.
var (
	ErrNotFound    = shared.ErrNotFound
	IsDuplicateKey = shared.IsDuplicateKey
)
