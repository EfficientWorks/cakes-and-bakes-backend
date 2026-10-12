-- +goose Up
-- +goose StatementBegin

/*
 * A business's own face on its website and emails.
 *
 * logo_url is the logo shown in the shop header and on every email and
 * invoice the business sends. website_url is the origin its shop is served
 * from (https://cakesandbakes.co.uk): email links point there, and a browser
 * request from that origin is recognised as this business's shop. One origin
 * belongs to one business.
 */
ALTER TABLE businesses ADD COLUMN logo_url    TEXT;
ALTER TABLE businesses ADD COLUMN website_url TEXT;
CREATE UNIQUE INDEX businesses_website_url_key ON businesses (lower(website_url)) WHERE website_url IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS businesses_website_url_key;
ALTER TABLE businesses DROP COLUMN IF EXISTS website_url;
ALTER TABLE businesses DROP COLUMN IF EXISTS logo_url;

-- +goose StatementEnd
