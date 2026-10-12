-- +goose Up

-- ---------------------------------------------------------------------------
-- Products: a seasonal on-sale window (CAT-09, the console's
-- "Visibility & scheduling" card)
-- ---------------------------------------------------------------------------

-- Categories got these in 0022 and branch_product_rules has carried them since
-- 0009, but the product master never did — so the console collected the dates
-- and had nowhere to put them. A product sold only for Mother's Day is a
-- property of the product, not of one branch's rule row, which is why it
-- belongs here rather than being written once per branch.
--
-- Null on both means "always on sale", which is what every existing row
-- becomes, so this changes nothing about the catalogue as it stands.
ALTER TABLE products
    ADD COLUMN available_from  DATE,
    ADD COLUMN available_until DATE;

-- The storefront grid filters on this window on every query, so the partial
-- index covers exactly the rows that have one — which is the minority.
CREATE INDEX idx_products_availability_window ON products (available_from, available_until)
    WHERE available_from IS NOT NULL OR available_until IS NOT NULL;

-- ---------------------------------------------------------------------------
-- Uploaded images live in the database
-- ---------------------------------------------------------------------------

-- The local storer writes to the container's own filesystem, which on Railway
-- is ephemeral: every deploy replaces the container and takes the product
-- photography with it. Cloudflare Images is the right answer at scale and the
-- Storer interface is already there for it, but it needs an account that does
-- not exist yet — and a catalogue whose images vanish on each deploy cannot be
-- demonstrated.
--
-- Bytes in Postgres is an honest middle: it survives redeploys, needs no third
-- party, and is bounded by MaxUploadBytes (10 MiB) per row. It is not where a
-- large catalogue should end up — swap in the Cloudflare storer and this table
-- simply stops being written to.
CREATE TABLE media_objects (
    -- 32 lowercase hex characters, issued by pkg/media. Not a UUID column
    -- because the id is minted by the application and appears verbatim in the
    -- delivery URL.
    id           TEXT PRIMARY KEY,
    content_type TEXT NOT NULL,
    byte_size    INTEGER NOT NULL,
    bytes        BYTEA NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down

DROP TABLE IF EXISTS media_objects;
DROP INDEX IF EXISTS idx_products_availability_window;
ALTER TABLE products
    DROP COLUMN IF EXISTS available_until,
    DROP COLUMN IF EXISTS available_from;
