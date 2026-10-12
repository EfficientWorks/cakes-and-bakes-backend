// Package store is the business service's database layer. It has one topic,
// so the facade simply embeds it; the layout matches the other services.
package store

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"bakehouse/services/business/internal/store/business"
	"bakehouse/services/business/internal/store/shared"
)

type businessRepo = business.Repo

// Store is every business repo behind one value.
type Store struct {
	*businessRepo
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{businessRepo: business.New(pool)}
}

// Errors the domain layer names as store.X.
var (
	ErrNotFound  = shared.ErrNotFound
	ErrDuplicate = shared.ErrDuplicate
)
