-- +goose Up
-- The till passcode, encrypted with a server key so admin can reveal it.
-- passcode_hash stays the sign-in check; codes set before this have no
-- sealed copy until they are set again.
ALTER TABLE staff_users ADD COLUMN passcode_sealed TEXT;

-- +goose Down
ALTER TABLE staff_users DROP COLUMN IF EXISTS passcode_sealed;
