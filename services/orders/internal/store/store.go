// Package store is the orders service's database layer.
//
// Each folder holds one topic, split the same way everywhere:
//
//	repo.go      the topic's Repo, its constructor, and what the topic is for
//	queries.go   SQL text and SQL fragments, as named constants
//	read.go      lookups and lists
//	write.go     inserts, updates and deletes
//	scan.go      row -> model mapping
//	model.go     types that exist only for the store
//	helpers.go   small pure functions the topic needs
//
// Dependencies run one way: shared <- slot, bill, returns, recipient,
// customer <- order <- kds, pos. Store gathers the repos into the one value
// the domain layer holds, so callers write s.store.GetOrderByID whichever
// folder the query lives in.
package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"bakehouse/services/orders/internal/store/addresscache"
	"bakehouse/services/orders/internal/store/bill"
	"bakehouse/services/orders/internal/store/cart"
	"bakehouse/services/orders/internal/store/cash"
	"bakehouse/services/orders/internal/store/customer"
	"bakehouse/services/orders/internal/store/kds"
	"bakehouse/services/orders/internal/store/order"
	"bakehouse/services/orders/internal/store/pos"
	"bakehouse/services/orders/internal/store/recipient"
	"bakehouse/services/orders/internal/store/report"
	"bakehouse/services/orders/internal/store/returns"
	"bakehouse/services/orders/internal/store/slot"
)

// One alias per topic: every package names its type Repo, and embedding
// needs distinct field names.
type (
	addressCacheRepo = addresscache.Repo
	billRepo         = bill.Repo
	cartRepo         = cart.Repo
	cashRepo         = cash.Repo
	customerRepo     = customer.Repo
	kdsRepo          = kds.Repo
	orderRepo        = order.Repo
	posRepo          = pos.Repo
	recipientRepo    = recipient.Repo
	reportRepo       = report.Repo
	returnsRepo      = returns.Repo
	slotRepo         = slot.Repo
)

// Store is every orders repo behind one value.
type Store struct {
	pool *pgxpool.Pool

	*addressCacheRepo
	*billRepo
	*cartRepo
	*cashRepo
	*customerRepo
	*kdsRepo
	*orderRepo
	*posRepo
	*recipientRepo
	*reportRepo
	*returnsRepo
	*slotRepo
}

func NewStore(pool *pgxpool.Pool) *Store {
	bills := bill.New(pool)
	returnsR := returns.New(pool)
	customers := customer.New(pool)
	orders := order.New(pool, bills, returnsR)
	return &Store{
		pool:             pool,
		addressCacheRepo: addresscache.New(pool),
		billRepo:         bills,
		cartRepo:         cart.New(pool),
		cashRepo:         cash.New(pool),
		customerRepo:     customers,
		kdsRepo:          kds.New(pool, orders),
		orderRepo:        orders,
		posRepo:          pos.New(pool, orders, customers),
		recipientRepo:    recipient.New(pool),
		reportRepo:       report.New(pool),
		returnsRepo:      returnsR,
		slotRepo:         slot.New(pool),
	}
}

// BeginTx exposes a raw transaction for the rare service-layer operation
// (releasing a slot outside CreateOrderTx / AmendOrderTx) that needs one
// without a dedicated store method.
func (s *Store) BeginTx(ctx context.Context) (pgx.Tx, error) {
	return s.pool.Begin(ctx)
}

// Types, functions and errors the domain layer names as store.X.
type (
	CustomerDetails     = order.CustomerDetails
	CustomerExistsError = customer.CustomerExistsError
	KOTActor            = kds.KOTActor
	SlotKey             = slot.SlotKey
)

var (
	NewOrderReference = order.NewOrderReference
	ReserveSlot       = slot.ReserveSlot
	ReleaseSlot       = slot.ReleaseSlot
	SlotMapKey        = slot.SlotMapKey
)

var (
	ErrAmendmentClosed      = order.ErrAmendmentClosed
	ErrBillsSettled         = bill.ErrBillsSettled
	ErrCustomerContactTaken = customer.ErrCustomerContactTaken
	ErrCustomerNotFound     = customer.ErrCustomerNotFound
	ErrDuplicatePayment     = bill.ErrDuplicatePayment
	ErrKOTConflict          = kds.ErrKOTConflict
)
