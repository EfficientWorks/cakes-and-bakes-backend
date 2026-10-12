-- +goose Up
-- +goose StatementBegin
ALTER TABLE products
ADD COLUMN IF NOT EXISTS disclaimer TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE products DROP COLUMN IF EXISTS disclaimer;
-- +goose StatementEnd