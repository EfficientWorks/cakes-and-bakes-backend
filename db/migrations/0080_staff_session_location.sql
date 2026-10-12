-- +goose Up
-- +goose StatementBegin

/*
 * Which store (or central kitchen) a POS / KDS session signed in to.
 *
 * Carried in the access token as well, but the refresh token has to remember
 * it too: otherwise the first refresh forgets the store, and the per-request
 * check that logs a till out when its store is closed would stop working.
 * NULL for admin console sessions, which are not tied to one location.
 */
ALTER TABLE refresh_tokens ADD COLUMN IF NOT EXISTS location_branch_id  UUID;
ALTER TABLE refresh_tokens ADD COLUMN IF NOT EXISTS location_kitchen_id UUID;

-- +goose StatementEnd

-- +goose Down
ALTER TABLE refresh_tokens DROP COLUMN IF EXISTS location_kitchen_id;
ALTER TABLE refresh_tokens DROP COLUMN IF EXISTS location_branch_id;
