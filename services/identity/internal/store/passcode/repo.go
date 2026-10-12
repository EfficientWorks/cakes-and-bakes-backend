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
