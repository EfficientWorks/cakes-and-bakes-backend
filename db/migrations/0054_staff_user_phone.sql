-- +goose Up
-- +goose StatementBegin

ALTER TABLE staff_users ADD COLUMN IF NOT EXISTS phone TEXT;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE staff_users DROP COLUMN IF EXISTS phone;
-- +goose StatementEnd