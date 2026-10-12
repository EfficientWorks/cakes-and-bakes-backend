// Package pos holds the till's own writes.
//
// Deliberately thin. A counter is not a checkout: the operator has the
// customer in front of them and the price on the screen, so this inserts what
// it is given and computes the arithmetic, and leaves the judgement to the
// storefront path that needs it.
package pos

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"bakehouse/services/orders/internal/store/customer"
	"bakehouse/services/orders/internal/store/order"
)

// Repo reads and writes till order rows.
type Repo struct {
	pool      *pgxpool.Pool
	orders    *order.Repo
	customers *customer.Repo
}

func New(pool *pgxpool.Pool, orders *order.Repo, customers *customer.Repo) *Repo {
	return &Repo{pool: pool, orders: orders, customers: customers}
}
