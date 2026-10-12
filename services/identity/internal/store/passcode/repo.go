// Package passcode stores staff passcodes and decides who may sign in where with one.
//
// The POS and the KDS share the passcode pad but not the people behind it:
//
//	POS at a store    anyone at that store except central-kitchen staff,
//	                  plus brand-wide staff
//	KDS at a store    central-kitchen staff assigned to that store, plus a
//	                  super admin
//	KDS at a kitchen  central-kitchen staff assigned to that kitchen, plus a
//	                  super admin
package passcode

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo reads and writes staff passcode rows.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

/*
 * A brand-wide role covers every location of its own business only.
 *
 * Sign-in is not business-scoped by request (the business is not known until
 * the token is issued), so the location's own business decides: a super admin
 * of one business is not a candidate at another business's till or KDS, and
 * does not block that business's staff from choosing the same code. $1 is
 * the store or kitchen id; NULL business on both sides is the pre-business data.
 */
const (
	brandOfBranch  = `EXISTS (SELECT 1 FROM branches lb WHERE lb.id = $1 AND lb.business_id IS NOT DISTINCT FROM u.business_id)`
	brandOfKitchen = `EXISTS (SELECT 1 FROM central_kitchens lk WHERE lk.id = $1 AND lk.business_id IS NOT DISTINCT FROM u.business_id)`
)
