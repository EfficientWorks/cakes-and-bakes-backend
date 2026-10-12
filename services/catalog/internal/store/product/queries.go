package product

const productSelect = `
	SELECT p.id, p.category_id, c.slug, p.name, p.slug, p.description, p.base_price_cents, p.lead_time, p.lead_time_unit,
	       p.production_station, p.allergen_flags, p.dietary_flags, p.occasion_tags, p.freshness_note,
	       p.ingredients_note, p.returns_note, p.disclaimer, p.is_best_seller, p.is_active, p.sort_order,
	       p.popularity_score,
	       p.price_floor_cents, p.price_ceiling_cents,
	       p.available_from, p.available_until, p.created_at, p.updated_at,
	       (EXISTS (SELECT 1 FROM product_modifier_groups pmg WHERE pmg.product_id = p.id)
	        OR EXISTS (SELECT 1 FROM modifier_groups mg WHERE mg.product_id = p.id)) AS is_modifier,
	       p.show_on_pos, p.show_on_website, p.pricing_mode,
	       p.min_prep_time, p.min_prep_time_unit, p.max_prep_time, p.max_prep_time_unit,
	       p.video_url
	FROM products p JOIN product_categories c ON c.id = p.category_id
	WHERE p.deleted_at IS NULL`
