-- +goose Up

-- Email capture for "we don't deliver here yet" (SF-03): the customer sees
-- the nearest branches with honest distances and can leave an email for a
-- future-coverage notification.
CREATE TABLE zone_coverage_requests (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email       TEXT NOT NULL,
    postcode    TEXT NOT NULL,
    notified_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_zone_coverage_requests_postcode ON zone_coverage_requests (postcode);

-- +goose Down
DROP TABLE IF EXISTS zone_coverage_requests;
