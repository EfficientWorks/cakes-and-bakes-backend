-- +goose Up
-- Category tiles on the home page and the Shop Now tabs are photo-led, so a
-- category carries its own artwork rather than borrowing a product's image.
ALTER TABLE product_categories
    ADD COLUMN image_url TEXT,
    ADD COLUMN image_alt_text TEXT;

-- Seeded categories get the imagery already used on the storefront. These are
-- placeholders in the sense that the brand can replace them at any time; they
-- are not placeholders in the sense of being wrong.
UPDATE product_categories SET
    image_url      = '/images/categories/cupcake-bouquets.jpg',
    image_alt_text = 'A handcrafted cupcake bouquet arranged like flowers in a fluted vase'
WHERE slug = 'cupcake-bouquets';

UPDATE product_categories SET
    image_url      = '/images/categories/cupcake-boxes.jpg',
    image_alt_text = 'An open gift box of decorated cupcakes'
WHERE slug = 'cupcake-boxes';

UPDATE product_categories SET
    image_url      = '/images/categories/macaron-gift-boxes.jpg',
    image_alt_text = 'A ribboned box of assorted macarons'
WHERE slug = 'macaron-gift-boxes';

-- +goose Down
ALTER TABLE product_categories
    DROP COLUMN IF EXISTS image_alt_text,
    DROP COLUMN IF EXISTS image_url;
