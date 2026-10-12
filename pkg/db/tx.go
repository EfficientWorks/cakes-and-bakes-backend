package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// WithTx runs fn inside a single transaction, committing when fn returns
// nil and rolling back on any error or panic.
//
// This is the seam the transactional outbox depends on: a state change and
// the outbox row describing it must be written in the same transaction, or
// an event can be published for a change that never committed (or a change
// can commit with no event). Anything that emits a domain event goes
// through here.
//
//	err := db.WithTx(ctx, pool, func(tx pgx.Tx) error {
//	    if err := insertOrder(ctx, tx, o); err != nil {
//	        return err
//	    }
//	    return outbox.Write(ctx, tx, events.Event{...})
//	})
func WithTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			// Roll back before re-panicking so the connection is not
			// returned to the pool mid-transaction.
			_ = tx.Rollback(ctx)
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
