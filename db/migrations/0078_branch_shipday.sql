-- +goose Up
-- +goose StatementBegin

/*
 * Shipday, per outlet.
 *
 * Not every outlet delivers through Shipday: some run their own drivers. A
 * delivery order is sent to Shipday only when its outlet has this switched
 * on. Off by default, so a new outlet never starts dispatching to a courier
 * account by accident.
 */
ALTER TABLE branches ADD COLUMN IF NOT EXISTS shipday_enabled BOOLEAN NOT NULL DEFAULT false;

-- +goose StatementEnd

-- +goose Down
ALTER TABLE branches DROP COLUMN IF EXISTS shipday_enabled;
