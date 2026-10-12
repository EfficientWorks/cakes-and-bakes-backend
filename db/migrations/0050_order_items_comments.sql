-- +goose Up
-- +goose StatementBegin
ALTER TABLE order_items ADD COLUMN IF NOT EXISTS comments TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE order_items DROP COLUMN IF EXISTS comments;
-- +goose StatementEnd