-- +goose Up
-- +goose StatementBegin

/*
 * 1. Pickup and delivery hours, per store.
 *
 * A store had one set of opening hours, used for both pickup and delivery
 * slots, with one open–close range a day. Now each fulfilment has its own
 * hours, and a day can have several ranges (9:00–12:00 and 14:00–18:00):
 *
 *   [{"day":"monday","closed":false,"ranges":[{"open":"09:00","close":"12:00"}, …]}, …]
 *
 * NULL means "not set": slots then come from opening_hours, as before, so
 * nothing changes for a store until its hours are edited. same_fulfilment_hours
 * is admin's "Use the same hours for pickup & delivery".
 */
ALTER TABLE branches
    ADD COLUMN IF NOT EXISTS pickup_hours          JSONB,
    ADD COLUMN IF NOT EXISTS delivery_hours        JSONB,
    ADD COLUMN IF NOT EXISTS same_fulfilment_hours BOOLEAN NOT NULL DEFAULT TRUE;

/*
 * 2. Seasonal tags as records of their own.
 *
 * Tags were free text on each product (products.occasion_tags). Now a tag is
 * created once — with a status and, optionally, restricted dates — and mapped
 * to products. Filters and the gift finder match on tag ids.
 *
 * Restricted dates: a product mapped to an active tag cannot be ordered for a
 * slot inside [restricted_from, restricted_to]. These are wall-clock times
 * (TIMESTAMP, no zone) on purpose: they are read in each store's own time
 * zone, so "Nov 24 12:30 AM" means just after midnight locally at every store.
 * repeat_yearly applies the same dates every year.
 *
 * products.occasion_tags is kept, written from the mapped tags' slugs, so
 * anything still reading it sees the same values.
 */
CREATE TABLE IF NOT EXISTS seasonal_tags (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name             TEXT NOT NULL,
    slug             TEXT NOT NULL,
    description      TEXT,
    status           TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    restricted       BOOLEAN NOT NULL DEFAULT FALSE,
    restricted_from  TIMESTAMP,
    restricted_to    TIMESTAMP,
    repeat_yearly    BOOLEAN NOT NULL DEFAULT FALSE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at       TIMESTAMPTZ,
    CONSTRAINT seasonal_tags_window CHECK (
        NOT restricted OR (restricted_from IS NOT NULL AND restricted_to IS NOT NULL AND restricted_to > restricted_from))
);
CREATE UNIQUE INDEX IF NOT EXISTS seasonal_tags_slug_idx ON seasonal_tags (slug) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS product_seasonal_tags (
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    tag_id     UUID NOT NULL REFERENCES seasonal_tags(id) ON DELETE CASCADE,
    PRIMARY KEY (product_id, tag_id)
);
CREATE INDEX IF NOT EXISTS product_seasonal_tags_tag_idx ON product_seasonal_tags (tag_id);

-- Today's free-text tags become tags (no restricted dates), mapped to the same
-- products. "thank_you" and "thank-you" are one tag.
INSERT INTO seasonal_tags (name, slug)
SELECT DISTINCT ON (slug) initcap(replace(slug, '-', ' ')), slug
  FROM (SELECT lower(regexp_replace(trim(t), '[\s_]+', '-', 'g')) AS slug
          FROM products, unnest(occasion_tags) t
         WHERE deleted_at IS NULL AND trim(t) <> '') s
ON CONFLICT DO NOTHING;

INSERT INTO product_seasonal_tags (product_id, tag_id)
SELECT DISTINCT p.id, st.id
  FROM products p, unnest(p.occasion_tags) t
  JOIN seasonal_tags st ON st.slug = lower(regexp_replace(trim(t), '[\s_]+', '-', 'g')) AND st.deleted_at IS NULL
 WHERE p.deleted_at IS NULL
ON CONFLICT DO NOTHING;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS product_seasonal_tags;
DROP TABLE IF EXISTS seasonal_tags;
ALTER TABLE branches
    DROP COLUMN IF EXISTS same_fulfilment_hours,
    DROP COLUMN IF EXISTS delivery_hours,
    DROP COLUMN IF EXISTS pickup_hours;
-- +goose StatementEnd
