-- +goose Up
-- +goose StatementBegin

/*
 * Flavours: the home page's "Discover our flavours" cards (Chocolate, Vanilla,
 * Fruit, Indian-Inspired). Staff create a flavour once — name, short
 * description, photo, card colour, position — and map products to it. The
 * website shows the active flavours that have an active product, and each
 * card's "Shop now" lists that flavour's products (?flavour=<slug>).
 *
 * Separate from products.sponges (a fixed list) and from seasonal tags (which
 * carry restricted dates): a flavour is free text the business names itself.
 */
CREATE TABLE IF NOT EXISTS flavours (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id       UUID REFERENCES businesses (id),
    name              TEXT NOT NULL,
    slug              TEXT NOT NULL,
    description       TEXT,
    image_url         TEXT,
    image_alt_text    TEXT,
    -- The card's tint, "#RRGGBB"; NULL lets the website choose.
    background_colour TEXT CHECK (background_colour IS NULL OR background_colour ~ '^#[0-9A-Fa-f]{6}$'),
    sort_order        INTEGER NOT NULL DEFAULT 0,
    status            TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at        TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS flavours_business_slug_idx
    ON flavours (business_id, slug) NULLS NOT DISTINCT WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS flavours_business_idx ON flavours (business_id);

CREATE TABLE IF NOT EXISTS product_flavours (
    product_id UUID NOT NULL REFERENCES products (id) ON DELETE CASCADE,
    flavour_id UUID NOT NULL REFERENCES flavours (id) ON DELETE CASCADE,
    PRIMARY KEY (product_id, flavour_id)
);
CREATE INDEX IF NOT EXISTS product_flavours_flavour_idx ON product_flavours (flavour_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS product_flavours;
DROP TABLE IF EXISTS flavours;

-- +goose StatementEnd
