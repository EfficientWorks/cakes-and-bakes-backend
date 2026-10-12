package kdssettings

const kdsSettingsSelect = `
	SELECT id, scope_type, branch_id::text, central_kitchen_id::text, kds_enabled,
	       show_pickup, show_delivery, show_takeaway, show_orders_based_on_time, order_appear_minutes,
	       play_sound_on_new_ticket, ticket_layout, ticket_columns, header_colors_enabled,
	       pickup_color, delivery_color, takeaway_color, dark_theme,
	       calendar_low_from, calendar_low_to, calendar_medium_from, calendar_medium_to, calendar_high_from,
	       created_at, updated_at
	  FROM kds_settings`
