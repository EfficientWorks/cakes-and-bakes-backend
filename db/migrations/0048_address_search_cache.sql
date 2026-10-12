-- +goose Up

CREATE TABLE address_search_cache (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    query_norm  TEXT NOT NULL,
    results     JSONB NOT NULL,
    source      TEXT NOT NULL DEFAULT 'locationiq',
    fetched_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (query_norm)
);

CREATE INDEX idx_address_search_cache_query ON address_search_cache (query_norm);

-- +goose Down
DROP TABLE IF EXISTS address_search_cache;