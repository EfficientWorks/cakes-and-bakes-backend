// Package events carries domain events between services through a
// transactional outbox.
//
// The rule the outbox exists to enforce: an event is written in the same
// database transaction as the state change it describes. Publishing
// directly from application code after a commit can lose events (the
// process dies between commit and publish) or emit events for work that
// rolled back. Writing the event as a row in the same transaction makes the
// two atomic, and a relay goroutine publishes the rows afterwards.
//
// Delivery is therefore at-least-once: the relay can publish a row and die
// before marking it done. Every consumer must be idempotent.
package events

import (
	"context"
	"encoding/json"
	"time"
)

// Event is one domain event. Type is the routing key consumers match on,
// in "<aggregate>.<past-tense-verb>" form: order.placed, order.cancelled,
// catalog.availability_changed, branch.suspended.
type Event struct {
	ID            string
	Type          string
	AggregateType string
	AggregateID   string
	// BranchID scopes the event so a consumer can fan out per branch
	// without re-reading the aggregate. It is nil for brand-level events.
	BranchID    *string
	Payload     json.RawMessage
	CreatedAt   time.Time
	PublishedAt *time.Time
}

// NewEvent builds an Event with its payload marshalled. The id and
// timestamp are assigned by the database on insert.
func NewEvent(eventType, aggregateType, aggregateID string, payload any) (Event, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return Event{}, err
	}
	return Event{
		Type:          eventType,
		AggregateType: aggregateType,
		AggregateID:   aggregateID,
		Payload:       body,
	}, nil
}

// Scoped returns a copy of the event tagged with its branch.
func (e Event) Scoped(branchID string) Event {
	if branchID != "" {
		e.BranchID = &branchID
	}
	return e
}

// Publisher delivers an event to whatever is listening. Today that is the
// no-op publisher; when the realtime SSE hub lands it becomes the consumer,
// with no change to the services emitting events.
type Publisher interface {
	Publish(ctx context.Context, e Event) error
}

// NoopPublisher discards every event. It is the default until a consumer
// exists — the outbox rows are still written, so switching to a real
// publisher replays nothing and loses nothing.
type NoopPublisher struct{}

// Publish implements Publisher.
func (NoopPublisher) Publish(context.Context, Event) error { return nil }

// PublisherFunc adapts a plain function to Publisher, which is what tests
// and small in-process consumers usually want.
type PublisherFunc func(ctx context.Context, e Event) error

// Publish implements Publisher.
func (f PublisherFunc) Publish(ctx context.Context, e Event) error { return f(ctx, e) }
