-- +goose Up
-- +goose StatementBegin

/*
 * Two additive changes.
 *
 * 1. A till order keeps the city and postcode it was typed with. Until now
 *    only the street was stored on the order; city and postcode went to the
 *    linked customer's address book and nowhere else, so a POS update that
 *    changed the city without naming a customer was lost.
 *
 * 2. A product can carry one video, shown with its images on the website.
 *    The video itself lives in media_videos (like media_objects for images),
 *    stored EXTERNAL so a byte-range read does not decompress the whole file —
 *    browsers play video by asking for ranges.
 */

ALTER TABLE orders ADD COLUMN IF NOT EXISTS guest_customer_city     TEXT;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS guest_customer_postcode TEXT;

ALTER TABLE products ADD COLUMN IF NOT EXISTS video_url TEXT;

CREATE TABLE IF NOT EXISTS media_videos (
    id           TEXT PRIMARY KEY,
    content_type TEXT NOT NULL,
    byte_size    BIGINT NOT NULL,
    bytes        BYTEA NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE media_videos ALTER COLUMN bytes SET STORAGE EXTERNAL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS media_videos;
ALTER TABLE products DROP COLUMN IF EXISTS video_url;
ALTER TABLE orders DROP COLUMN IF EXISTS guest_customer_postcode;
ALTER TABLE orders DROP COLUMN IF EXISTS guest_customer_city;
-- +goose StatementEnd
