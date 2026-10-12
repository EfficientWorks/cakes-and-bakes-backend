package tag

const tagSelect = `
	SELECT t.id, t.name, t.slug, t.description, t.status, t.restricted, t.restricted_from, t.restricted_to,
	       t.repeat_yearly, t.created_at, t.updated_at,
	       (SELECT count(*) FROM product_seasonal_tags pt JOIN products p ON p.id = pt.product_id
	         WHERE pt.tag_id = t.id AND p.deleted_at IS NULL)
	  FROM seasonal_tags t
	 WHERE t.deleted_at IS NULL`
