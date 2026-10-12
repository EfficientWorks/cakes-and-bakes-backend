package kitchen

const centralKitchenSelect = `
	SELECT ck.id, ck.name, ck.location_code, ck.street, ck.city, ck.state, ck.postcode, ck.country,
	       ck.phone, ck.email, ck.timezone, ck.status,
	       COALESCE(ck.manager_name, ''), COALESCE(ck.manager_phone, ''), COALESCE(ck.manager_email, ''),
	       ck.created_at, ck.updated_at
	  FROM central_kitchens ck`
