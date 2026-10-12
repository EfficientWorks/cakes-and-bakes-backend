package events

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Defaults for the relay loop. Deliberately unaggressive: the outbox is a
// durability mechanism, not a low-latency transport. Realtime push to the
// KDS is a separate concern (ADR-0010).
const (
	defaultPollInterval = 2 * time.Second
	defaultBatchSize    = 100
)

// Relay drains the outbox and hands each event to a Publisher.
//
// It runs in-process rather than as a managed broker, which is the right
// trade at one instance (ADR-0004). The seam that would change if that
// stops being true is Publisher, not the services emitting events.
type Relay struct {
	outbox    *Outbox
	publisher Publisher
	log       *slog.Logger

	interval  time.Duration
	batchSize int

	stop chan struct{}
	done chan struct{}
	once sync.Once
}

// NewRelay returns a relay polling outbox and dispatching to publisher.
func NewRelay(outbox *Outbox, publisher Publisher, log *slog.Logger) *Relay {
	return &Relay{
		outbox:    outbox,
		publisher: publisher,
		log:       log,
		interval:  defaultPollInterval,
		batchSize: defaultBatchSize,
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
}

// Start begins the polling loop in its own goroutine. It returns
// immediately. The loop exits when ctx is cancelled or Stop is called.
func (r *Relay) Start(ctx context.Context) {
	go func() {
		defer close(r.done)
		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-r.stop:
				return
			case <-ticker.C:
				r.drain(ctx)
			}
		}
	}()
}

// Stop halts the loop and waits for the in-flight batch to finish.
func (r *Relay) Stop() {
	r.once.Do(func() { close(r.stop) })
	<-r.done
}

// drain publishes one batch. A failure to publish leaves the row
// unpublished so the next tick retries it — which is why consumers must be
// idempotent.
func (r *Relay) drain(ctx context.Context) {
	batch, err := r.outbox.Unpublished(ctx, r.batchSize)
	if err != nil {
		r.log.Error("outbox read failed", "error", err)
		return
	}
	if len(batch) == 0 {
		return
	}

	published := make([]string, 0, len(batch))
	for _, e := range batch {
		if err := r.publisher.Publish(ctx, e); err != nil {
			r.log.Error("event publish failed; will retry",
				"error", err, "event_type", e.Type, "event_id", e.ID)
			continue
		}
		published = append(published, e.ID)
	}

	if err := r.outbox.MarkPublished(ctx, published); err != nil {
		// The events were delivered but not marked. They will be delivered
		// again on the next tick — at-least-once, as documented.
		r.log.Error("marking events published failed", "error", err, "count", len(published))
		return
	}
	r.log.Debug("outbox batch published", "count", len(published))
}
