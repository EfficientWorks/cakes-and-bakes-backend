package category

// categoryAdminList returns the business's categories ($1) with their enabled
// branch IDs aggregated, so the admin list renders branch visibility in one
// round trip.
const categoryAdminList = `
	SELECT pc.id, pc.name, pc.slug, pc.description, pc.image_url, pc.image_alt_text,
	       pc.sort_order, pc.is_active, pc.available_from, pc.available_until, pc.color_code,
	       COALESCE(ARRAY_AGG(bcr.branch_id ORDER BY bcr.branch_id)
	                FILTER (WHERE bcr.branch_enabled = true), '{}') AS branch_ids
	FROM product_categories pc
	LEFT JOIN branch_category_rules bcr ON bcr.category_id = pc.id
	WHERE pc.business_id IS NOT DISTINCT FROM $1` + categoryAdminGroupBy

const categoryAdminGroupBy = `
	GROUP BY pc.id, pc.name, pc.slug, pc.description, pc.image_url, pc.image_alt_text,
	         pc.sort_order, pc.is_active, pc.available_from, pc.available_until, pc.color_code`

// categoryMappedToBranches keeps only categories enabled at one of the given
// branches — what a branch login is allowed to see in the console.
const categoryMappedToBranches = `
	SELECT pc.id, pc.name, pc.slug, pc.description, pc.image_url, pc.image_alt_text,
	       pc.sort_order, pc.is_active, pc.available_from, pc.available_until, pc.color_code,
	       COALESCE(ARRAY_AGG(bcr.branch_id ORDER BY bcr.branch_id)
	                FILTER (WHERE bcr.branch_enabled = true), '{}') AS branch_ids
	FROM product_categories pc
	LEFT JOIN branch_category_rules bcr ON bcr.category_id = pc.id
	WHERE pc.business_id IS NOT DISTINCT FROM $1
	  AND EXISTS (SELECT 1 FROM branch_category_rules m
	               WHERE m.category_id = pc.id AND m.branch_enabled AND m.branch_id::text = ANY($2))` + categoryAdminGroupBy
