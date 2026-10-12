// Package cash stores the cash drawer: the float, pay-ins and pay-outs, and the day close.
//
// A business date here is the date at the outlet, not UTC: a store trading
// into the evening is still on the same day when the server has rolled over,
// and a day close that split the evening across two dates would be wrong in
// a way nobody would find until the numbers were audited. Hence
// AT TIME ZONE b.timezone in every takings query.
package cash

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo reads and writes cash drawer rows.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }
