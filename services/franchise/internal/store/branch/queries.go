package branch

const branchSelect = `
	       SELECT b.id, b.name, b.code, b.type, b.ownership_type, b.street, b.city, b.state, b.postcode, b.country, b.latitude, b.longitude,
	       b.phone, b.email,
	       b.timezone, b.opening_hours, b.fulfilment_types, b.delivery_zone, b.delivery_fee_cents, b.min_order_cents,
	       b.tax_name, b.tax_rate_pct, b.accepts_cash_on_delivery, b.cod_max_order_cents, b.delivery_pricing,
	       b.card_providers, b.pickup_hours, b.delivery_hours, b.same_fulfilment_hours, b.status, b.emergency_closed_on, b.location_code, b.created_at, b.updated_at,
	       b.business_id
	FROM branches b`
