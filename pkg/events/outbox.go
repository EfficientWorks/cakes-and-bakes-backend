package events

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Outbox reads and writes the domain_events table.
type Outbox struct {
	pool *pgxpool.Pool
}

// NewOutbox returns an Outbox backed by pool.
func NewOutbox(pool *pgxpool.Pool) *Outbox { return &Outbox{pool: pool} }

// Write appends an event inside the caller's transaction. It takes a pgx.Tx
// rather than the pool precisely so it cannot be called outside one — see
// db.WithTx.
func (o *Outbox) Write(ctx context.Context, tx pgx.Tx, e Event) error {
	const q = `
		INSERT INTO domain_events (event_type, aggregate_type, aggregate_id, branch_id, payload)
		VALUES ($1, $2, $3, $4, $5)`
	if _, err := tx.Exec(ctx, q, e.Type, e.AggregateType, e.AggregateID, e.BranchID, e.Payload); err != nil {
		return fmt.Errorf("write outbox event %s: %w", e.Type, err)
	}
	return nil
}

// Unpublished returns up to limit events that have not been dispatched yet,
// oldest first.
//
// FOR UPDATE SKIP LOCKED means a second relay (a rolling deploy running two
// instances briefly) picks up different rows rather than blocking on or
// duplicating the first one's batch.
func (o *Outbox) Unpublished(ctx context.Context, limit int) ([]Event, error) {
	const q = `
		SELECT id::text, event_type, aggregate_type, aggregate_id, branch_id::text, payload, created_at
		FROM domain_events
		WHERE published_at IS NULL
		ORDER BY created_at
		LIMIT $1
		FOR UPDATE SKIP LOCKED`

	rows, err := o.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("query outbox: %w", err)
	}
	defer rows.Close()

	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.Type, &e.AggregateType, &e.AggregateID,
			&e.BranchID, &e.Payload, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan outbox row: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// MarkPublished stamps the given events as dispatched.
func (o *Outbox) MarkPublished(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	const q = `UPDATE domain_events SET published_at = now() WHERE id = ANY($1::uuid[])`
	if _, err := o.pool.Exec(ctx, q, ids); err != nil {
		return fmt.Errorf("mark outbox events published: %w", err)
	}
	return nil
}
