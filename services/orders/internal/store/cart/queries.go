package cart

const cartSelect = `
	SELECT id, branch_id, customer_id, fulfilment_type, delivery_street, delivery_city, delivery_state,
	       delivery_postcode, scheduled_date, slot_start::text, slot_end::text, discount_code, tip_cents,
	       created_at, updated_at, delivery_latitude, delivery_longitude
	FROM carts`
