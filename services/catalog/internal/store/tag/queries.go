package tag

const tagSelect = `
	SELECT t.id, t.name, t.slug, t.description, t.status, t.restricted, t.restricted_from, t.restricted_to,
	       t.repeat_yearly, t.created_at, t.updated_at,
	       (SELECT count(*) FROM product_seasonal_tags pt JOIN products p ON p.id = pt.product_id
	         WHERE pt.tag_id = t.id AND p.deleted_at IS NULL)
	  FROM seasonal_tags t
	 WHERE t.deleted_at IS NULL`

// tagScoped is tagSelect limited to the request's business. $1 is always
// tenant.BusinessParam(ctx); a caller's own arguments start at $2.
const tagScoped = tagSelect + ` AND t.business_id IS NOT DISTINCT FROM $1`
