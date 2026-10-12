-- +goose Up

-- Seed data. This is representative content for local dev / Postman
-- testing only — the real catalogue is a critical, still-outstanding
-- client dependency (BRD Section 20.2, item 1: "Full product list —
-- names, sizes, flavours, add-ons, prices"). Prices/options below are
-- taken directly from the approved Figma product-detail screens for
-- Berry Blossom Cupcake Bouquet; the other three products are simplified
-- placeholders in the same shape.

INSERT INTO product_categories (id, name, slug, description, sort_order) VALUES
    ('11111111-0000-0000-0000-000000000001', 'Cupcake Bouquets',  'cupcake-bouquets',  'Handcrafted cupcake bouquets arranged like flowers.', 1),
    ('11111111-0000-0000-0000-000000000002', 'Cupcake Boxes',     'cupcake-boxes',     'Boxed cupcake gift sets.', 2),
    ('11111111-0000-0000-0000-000000000003', 'Macaron Gift Boxes','macaron-gift-boxes','Assorted macaron gift sets.', 3);

-- +goose StatementBegin
DO $$
DECLARE
    berry_blossom_id UUID := gen_random_uuid();
    golden_glow_id   UUID := gen_random_uuid();
    royal_bloom_id   UUID := gen_random_uuid();
    macaron_12_id    UUID := gen_random_uuid();
    grp_id           UUID;
BEGIN
    -- ============ Berry Blossom Cupcake Bouquet (fully detailed) ============
    INSERT INTO products (id, category_id, name, slug, description, base_price_cents, lead_time_hours,
        production_station, allergen_flags, occasion_tags, freshness_note, ingredients_note, returns_note,
        is_best_seller, sort_order)
    VALUES (berry_blossom_id, '11111111-0000-0000-0000-000000000001', 'Berry Blossom Cupcake Bouquet',
        'berry-blossom-cupcake-bouquet',
        'Raspberry ruffles and blush roses piped by hand, arranged in a reusable fluted vase.',
        6800, 4, 'bouquet_station', ARRAY['dairy','eggs','gluten'], ARRAY['birthday','anniversary'],
        'Made fresh daily.', 'Vanilla sponge, buttercream, seasonal berries. See allergen flags for full list.',
        'Due to the perishable nature of our products, returns are not accepted once delivered — see our Returns Policy.',
        true, 1);

    INSERT INTO product_images (product_id, url, alt_text, type, is_primary, sort_order) VALUES
        (berry_blossom_id, 'https://media.bakedbouquet.com/products/berry-blossom-1.jpg', 'Berry Blossom Cupcake Bouquet', 'studio', true, 1),
        (berry_blossom_id, 'https://media.bakedbouquet.com/products/berry-blossom-2.jpg', 'Berry Blossom Cupcake Bouquet, side angle', 'studio', false, 2);

    INSERT INTO product_variants (product_id, name, label, price_delta_cents, serves_min, serves_max, sort_order) VALUES
        (berry_blossom_id, '6 Cupcakes',  'Best Deal · Serves 3-4',        0,    3, 4,  1),
        (berry_blossom_id, '9 Cupcakes',  'Mini Gifted · Serves 5-7',      2000, 5, 7,  2),
        (berry_blossom_id, '12 Cupcakes', 'Table Centerpiece',             4100, 8, 11, 3),
        (berry_blossom_id, '15 Cupcakes', 'Showstopper · Serves 12+',      6100, 12, 20, 4);

    INSERT INTO modifier_groups (id, product_id, name, type, is_required, min_select, max_select, sort_order)
    VALUES (gen_random_uuid(), berry_blossom_id, 'Flavours', 'flavour', true, 1, 3, 1)
    RETURNING id INTO grp_id;
    INSERT INTO modifier_options (modifier_group_id, name, price_delta_cents, is_gluten_free, sort_order) VALUES
        (grp_id, 'Vanilla',               0,   false, 1),
        (grp_id, 'Chocolate',             0,   false, 2),
        (grp_id, 'Red Velvet',            0,   false, 3),
        (grp_id, 'Strawberry',            0,   false, 4),
        (grp_id, 'Gluten Free Vanilla',   300, true,  5),
        (grp_id, 'Gluten Free Chocolate', 300, true,  6);

    INSERT INTO modifier_groups (id, product_id, name, type, is_required, min_select, max_select, sort_order)
    VALUES (gen_random_uuid(), berry_blossom_id, 'Add a little extra', 'addon', false, 0, 6, 2)
    RETURNING id INTO grp_id;
    INSERT INTO modifier_options (modifier_group_id, name, price_delta_cents, sort_order) VALUES
        (grp_id, 'It''s a Girl',      300, 1),
        (grp_id, 'It''s a Boy',       300, 2),
        (grp_id, 'Birthday',          300, 3),
        (grp_id, 'Congratulations',   300, 4),
        (grp_id, 'Butterfly Topper',  300, 5),
        (grp_id, 'Macaron 06 Pack',   300, 6);

    INSERT INTO modifier_groups (id, product_id, name, type, is_required, min_select, max_select, sort_order)
    VALUES (gen_random_uuid(), berry_blossom_id, 'Presentation', 'presentation', false, 0, 1, 3)
    RETURNING id INTO grp_id;
    INSERT INTO modifier_options (modifier_group_id, name, price_delta_cents, sort_order) VALUES
        (grp_id, 'Ceramic Vase',  0,    1),
        (grp_id, 'Gift Box',      600,  2),
        (grp_id, 'Woven Basket',  1200, 3);

    INSERT INTO modifier_groups (id, product_id, name, type, is_required, min_select, max_select, sort_order)
    VALUES (gen_random_uuid(), berry_blossom_id, 'Wrap Colour', 'wrap_colour', false, 0, 1, 4)
    RETURNING id INTO grp_id;
    INSERT INTO modifier_options (modifier_group_id, name, price_delta_cents, colour_hex, sort_order) VALUES
        (grp_id, 'Pink',     0, '#F9C0C0', 1),
        (grp_id, 'Lavender', 0, '#C8B4E8', 2),
        (grp_id, 'Gold',     0, '#F5D998', 3),
        (grp_id, 'Purple',   0, '#4B0082', 4),
        (grp_id, 'Sage',     0, '#B2C9B4', 5);

    -- ============ Golden Glow Cupcake Bouquet (simplified) ============
    INSERT INTO products (id, category_id, name, slug, description, base_price_cents, lead_time_hours,
        production_station, allergen_flags, occasion_tags, is_best_seller, sort_order)
    VALUES (golden_glow_id, '11111111-0000-0000-0000-000000000001', 'Golden Glow Cupcake Bouquet',
        'golden-glow-cupcake-bouquet', 'Sunflower-yellow buttercream roses on a bed of gold leaf accents.',
        6800, 4, 'bouquet_station', ARRAY['dairy','eggs','gluten'], ARRAY['birthday','thank_you'], false, 2);

    INSERT INTO product_images (product_id, url, type, is_primary, sort_order) VALUES
        (golden_glow_id, 'https://media.bakedbouquet.com/products/golden-glow-1.jpg', 'studio', true, 1);

    INSERT INTO product_variants (product_id, name, label, price_delta_cents, serves_min, serves_max, sort_order) VALUES
        (golden_glow_id, '6 Cupcakes',  'Best Deal · Serves 3-4',   0,    3, 4,  1),
        (golden_glow_id, '9 Cupcakes',  'Mini Gifted · Serves 5-7', 2000, 5, 7,  2),
        (golden_glow_id, '12 Cupcakes', 'Table Centerpiece',        4100, 8, 11, 3),
        (golden_glow_id, '15 Cupcakes', 'Showstopper · Serves 12+', 6100, 12, 20, 4);

    INSERT INTO modifier_groups (id, product_id, name, type, is_required, min_select, max_select, sort_order)
    VALUES (gen_random_uuid(), golden_glow_id, 'Flavours', 'flavour', true, 1, 3, 1)
    RETURNING id INTO grp_id;
    INSERT INTO modifier_options (modifier_group_id, name, price_delta_cents, is_gluten_free, sort_order) VALUES
        (grp_id, 'Vanilla',             0,   false, 1),
        (grp_id, 'Chocolate',           0,   false, 2),
        (grp_id, 'Gluten Free Vanilla', 300, true,  3);

    -- ============ Royal Bloom Cupcake Box (Cupcake Boxes category) ============
    INSERT INTO products (id, category_id, name, slug, description, base_price_cents, lead_time_hours,
        production_station, allergen_flags, occasion_tags, sort_order)
    VALUES (royal_bloom_id, '11111111-0000-0000-0000-000000000002', 'Royal Bloom Cupcake Box',
        'royal-bloom-cupcake-box', 'Six cupcakes boxed with a window lid and ribbon, ready to gift.',
        5300, 3, 'box_station', ARRAY['dairy','eggs','gluten'], ARRAY['thank_you','sympathy'], 1);

    INSERT INTO product_images (product_id, url, type, is_primary, sort_order) VALUES
        (royal_bloom_id, 'https://media.bakedbouquet.com/products/royal-bloom-1.jpg', 'studio', true, 1);

    INSERT INTO product_variants (product_id, name, label, price_delta_cents, serves_min, serves_max, sort_order) VALUES
        (royal_bloom_id, '4 Cupcakes',  'Petite',        0,    2, 3, 1),
        (royal_bloom_id, '6 Cupcakes',  'Classic',       1500, 3, 4, 2),
        (royal_bloom_id, '12 Cupcakes', 'Share Box',     4200, 8, 11, 3);

    INSERT INTO modifier_groups (id, product_id, name, type, is_required, min_select, max_select, sort_order)
    VALUES (gen_random_uuid(), royal_bloom_id, 'Flavours', 'flavour', true, 1, 3, 1)
    RETURNING id INTO grp_id;
    INSERT INTO modifier_options (modifier_group_id, name, price_delta_cents, is_gluten_free, sort_order) VALUES
        (grp_id, 'Vanilla',   0, false, 1),
        (grp_id, 'Chocolate', 0, false, 2);

    -- ============ Macaron 12 Pack (Macaron Gift Boxes category) ============
    INSERT INTO products (id, category_id, name, slug, description, base_price_cents, lead_time_hours,
        production_station, allergen_flags, dietary_flags, occasion_tags, sort_order)
    VALUES (macaron_12_id, '11111111-0000-0000-0000-000000000003', 'Macaron 12 Pack',
        'macaron-12-pack', 'Assorted French macarons, 12 count.',
        3800, 3, 'macaron_station', ARRAY['dairy','eggs','nuts'], ARRAY['gluten_free'], ARRAY['thank_you','birthday'], 1);

    INSERT INTO product_images (product_id, url, type, is_primary, sort_order) VALUES
        (macaron_12_id, 'https://media.bakedbouquet.com/products/macaron-12-1.jpg', 'studio', true, 1);

    INSERT INTO product_variants (product_id, name, label, price_delta_cents, serves_min, serves_max, sort_order) VALUES
        (macaron_12_id, '6 Pack',  'Petite',  0,    2, 3, 1),
        (macaron_12_id, '12 Pack', 'Classic', 2100, 4, 6, 2);

    INSERT INTO modifier_groups (id, product_id, name, type, is_required, min_select, max_select, sort_order)
    VALUES (gen_random_uuid(), macaron_12_id, 'Flavour Assortment', 'flavour', true, 1, 1, 1)
    RETURNING id INTO grp_id;
    INSERT INTO modifier_options (modifier_group_id, name, price_delta_cents, sort_order) VALUES
        (grp_id, 'Classic Assortment', 0, 1),
        (grp_id, 'Chocolate Lovers',   0, 2);
END $$;
-- +goose StatementEnd

-- +goose Down
DELETE FROM products WHERE slug IN ('berry-blossom-cupcake-bouquet','golden-glow-cupcake-bouquet','royal-bloom-cupcake-box','macaron-12-pack');
DELETE FROM product_categories WHERE id IN (
    '11111111-0000-0000-0000-000000000001',
    '11111111-0000-0000-0000-000000000002',
    '11111111-0000-0000-0000-000000000003'
);
