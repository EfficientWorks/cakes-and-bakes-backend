// Package bill stores split bills and the payments taken against them.
//
// Three ways to reach the same place: split the total into equal or chosen
// amounts, by whose items are whose, or by how each share is being paid.
// However it was arrived at, what comes out is a set of amounts that add up
// to the order, and from then on each one is just something somebody owes.
// The mode is kept so a receipt can explain itself, not so the settling code
// has to branch on it.
package bill

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo reads and writes bill rows.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }
