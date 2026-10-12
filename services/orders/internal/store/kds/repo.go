// Package kds holds the Kitchen Display's reads and writes.
//
// Reads are scoped by store ids the caller has already been authorised for
// (the domain resolves a central kitchen to its stores). Writes are
// conditional on the status the caller saw, so two screens pressing the same
// button cannot both move the ticket.
//
// POS "Start Process" on an order for a later day releases it to the kitchen
// early (release.go): it shows on today's board while the till keeps it on
// the Scheduled tab until its day.
package kds

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"bakehouse/services/orders/internal/store/order"
)

// Repo reads and writes kitchen display rows.
type Repo struct {
	pool   *pgxpool.Pool
	orders *order.Repo
}

func New(pool *pgxpool.Pool, orders *order.Repo) *Repo { return &Repo{pool: pool, orders: orders} }
