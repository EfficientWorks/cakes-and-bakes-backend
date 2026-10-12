// Package slot stores each branch's scheduling config and its time slots with their capacity.
//
// Capacity is reserved and released inside the order's own transaction
// (ReserveSlot / ReleaseSlot), so a slot can never be oversold.
package slot

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo reads and writes slot rows.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }
