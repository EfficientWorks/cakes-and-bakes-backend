package kitchen

const centralKitchenSelect = `
	SELECT ck.id, ck.name, ck.location_code, ck.street, ck.city, ck.state, ck.postcode, ck.country,
	       ck.phone, ck.email, ck.timezone, ck.status,
	       COALESCE(ck.manager_name, ''), COALESCE(ck.manager_phone, ''), COALESCE(ck.manager_email, ''),
	       ck.created_at, ck.updated_at
	  FROM central_kitchens ck`

// kitchenScoped limits a query to the caller's kitchens. $%d is the business
// parameter (tenant.BusinessParam).
const kitchenScoped = `ck.business_id IS NOT DISTINCT FROM $%d`
