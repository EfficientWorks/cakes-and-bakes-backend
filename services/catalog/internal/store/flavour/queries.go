package flavour

// flavourSelect reads a flavour with its count of mapped, undeleted products.
const flavourSelect = `
	SELECT f.id, f.name, f.slug, f.description, f.image_url, f.image_alt_text, f.background_colour,
	       f.sort_order, f.status, f.created_at, f.updated_at,
	       (SELECT count(*) FROM product_flavours pf JOIN products p ON p.id = pf.product_id
	         WHERE pf.flavour_id = f.id AND p.deleted_at IS NULL)
	  FROM flavours f
	 WHERE f.deleted_at IS NULL`

// flavourScoped is flavourSelect limited to the request's business. $1 is
// always tenant.BusinessParam(ctx); a caller's own arguments start at $2.
const flavourScoped = flavourSelect + ` AND f.business_id IS NOT DISTINCT FROM $1`

// flavourOrder is the cards' order: staff's position, then name.
const flavourOrder = ` ORDER BY f.sort_order, lower(f.name)`

// storefrontSelect is the active flavours with at least one active product,
// counting only those.
const storefrontSelect = `
	SELECT f.id, f.name, f.slug, f.description, f.image_url, f.image_alt_text, f.background_colour,
	       f.sort_order, f.status, f.created_at, f.updated_at, live.n
	  FROM flavours f
	  JOIN LATERAL (SELECT count(*) AS n FROM product_flavours pf JOIN products p ON p.id = pf.product_id
	                 WHERE pf.flavour_id = f.id AND p.deleted_at IS NULL AND p.is_active = true) live ON live.n > 0
	 WHERE f.deleted_at IS NULL AND f.status = 'active' AND f.business_id IS NOT DISTINCT FROM $1`
