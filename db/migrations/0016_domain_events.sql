-- +goose Up
-- Transactional outbox. A domain event is inserted in the same transaction
-- as the state change it describes, then relayed out-of-band. Delivery is
-- at-least-once, so every consumer must be idempotent.
CREATE TABLE domain_events (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type     TEXT        NOT NULL,
    aggregate_type TEXT        NOT NULL,
    aggregate_id   TEXT        NOT NULL,
    franchise_id   UUID,
    branch_id      UUID,
    payload        JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at   TIMESTAMPTZ
);

-- The relay's only query: oldest unpublished first. Partial index keeps it
-- proportional to the backlog rather than to the whole event history.
CREATE INDEX idx_domain_events_unpublished
    ON domain_events (created_at)
    WHERE published_at IS NULL;

CREATE INDEX idx_domain_events_aggregate ON domain_events (aggregate_type, aggregate_id);
CREATE INDEX idx_domain_events_branch    ON domain_events (branch_id, created_at);

-- +goose Down
DROP TABLE IF EXISTS domain_events;
