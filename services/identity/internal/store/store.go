// Package store is the identity service's database layer.
//
// Each folder holds one topic: its Repo (repo.go, with what the topic is
// for), and its queries, reads, writes and row scanning in their own files.
// Store gathers the topic repos into the one value the domain layer holds,
// so callers write s.store.X whichever folder the query lives in. Errors and
// helpers shared across topics live in store/shared.
package store

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"bakehouse/services/identity/internal/store/address"
	"bakehouse/services/identity/internal/store/customer"
	"bakehouse/services/identity/internal/store/passcode"
	"bakehouse/services/identity/internal/store/recipient"
	"bakehouse/services/identity/internal/store/session"
	"bakehouse/services/identity/internal/store/shared"
	"bakehouse/services/identity/internal/store/staff"
)

// One alias per topic: every package names its type Repo, and embedding
// needs distinct field names.
type (
	addressRepo   = address.Repo
	customerRepo  = customer.Repo
	passcodeRepo  = passcode.Repo
	recipientRepo = recipient.Repo
	sessionRepo   = session.Repo
	staffRepo     = staff.Repo
)

// Store is every identity repo behind one value.
type Store struct {
	*addressRepo
	*customerRepo
	*passcodeRepo
	*recipientRepo
	*sessionRepo
	*staffRepo
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{
		addressRepo:   address.New(pool),
		customerRepo:  customer.New(pool),
		passcodeRepo:  passcode.New(pool),
		recipientRepo: recipient.New(pool),
		sessionRepo:   session.New(pool),
		staffRepo:     staff.New(pool),
	}
}

// Names the domain layer uses as store.X.
var (
	ErrNotFound   = shared.ErrNotFound
	ErrEmailTaken = shared.ErrEmailTaken
)
