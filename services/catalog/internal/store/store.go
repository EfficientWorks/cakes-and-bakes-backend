// Package store is the catalog service's database layer.
//
// Each folder holds one topic, split the same way everywhere:
//
//	repo.go      the topic's Repo and its constructor
//	queries.go   SQL text, as named constants
//	read.go      lookups and lists
//	write.go     inserts, updates and deletes
//	scan.go      row -> model mapping
//	model.go     types that exist only for the store
//	helpers.go   small pure functions the topic needs
//
// Store gathers the topic repos into the one value the domain layer holds, so
// callers write s.store.GetProductByID whichever folder the query lives in.
// Errors and helpers shared across topics live in store/shared.
package store

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"bakehouse/services/catalog/internal/store/availability"
	"bakehouse/services/catalog/internal/store/category"
	"bakehouse/services/catalog/internal/store/content"
	"bakehouse/services/catalog/internal/store/coupon"
	"bakehouse/services/catalog/internal/store/modifier"
	"bakehouse/services/catalog/internal/store/product"
	"bakehouse/services/catalog/internal/store/shared"
	"bakehouse/services/catalog/internal/store/tag"
)

// One alias per topic: every package names its type Repo, and embedding
// needs distinct field names.
type (
	availabilityRepo = availability.Repo
	categoryRepo     = category.Repo
	contentRepo      = content.Repo
	couponRepo       = coupon.Repo
	modifierRepo     = modifier.Repo
	productRepo      = product.Repo
	tagRepo          = tag.Repo
)

// Store is every catalog repo behind one value.
type Store struct {
	*availabilityRepo
	*categoryRepo
	*contentRepo
	*couponRepo
	*modifierRepo
	*productRepo
	*tagRepo
}

func NewStore(pool *pgxpool.Pool) *Store {
	tags := tag.New(pool)
	return &Store{
		availabilityRepo: availability.New(pool),
		categoryRepo:     category.New(pool),
		contentRepo:      content.New(pool),
		couponRepo:       coupon.New(pool),
		modifierRepo:     modifier.New(pool),
		productRepo:      product.New(pool, tags),
		tagRepo:          tags,
	}
}

// Types and errors the domain layer names as store.X.
type (
	AdminProductQuery      = product.AdminProductQuery
	CategoryRow            = category.CategoryRow
	StorefrontCouponFilter = coupon.StorefrontCouponFilter
)

var (
	ErrNotFound       = shared.ErrNotFound
	ErrDuplicate      = shared.ErrDuplicate
	ErrRelatedMissing = shared.ErrRelatedMissing
	ErrMalformedID    = shared.ErrMalformedID
)
