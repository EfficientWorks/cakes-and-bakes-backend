-- +goose Up
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- +goose Down
-- extensions are left in place on rollback
