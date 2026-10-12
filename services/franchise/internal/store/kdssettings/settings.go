package kdssettings

import (
	"context"
	"errors"

	"bakehouse/services/franchise/franchiseapi"
	"bakehouse/services/franchise/internal/store/shared"
)

// EnsureKDSSettings creates a location's settings row at defaults if it has
// none. Idempotent.
func (s *Repo) EnsureKDSSettings(ctx context.Context, scope franchiseapi.KDSScope) error {
	var err error
	if scope.Type == franchiseapi.LocationCentralKitchen {
		_, err = s.pool.Exec(ctx, `
			INSERT INTO kds_settings (scope_type, central_kitchen_id) VALUES ('central_kitchen', $1)
			ON CONFLICT DO NOTHING`, scope.CentralKitchenID)
	} else {
		_, err = s.pool.Exec(ctx, `
			INSERT INTO kds_settings (scope_type, branch_id) VALUES ('branch', $1)
			ON CONFLICT DO NOTHING`, scope.BranchID)
	}
	return err
}

// KDSSettingsFor reads a location's settings, creating them at defaults the
// first time they are asked for.
func (s *Repo) KDSSettingsFor(ctx context.Context, scope franchiseapi.KDSScope) (*franchiseapi.KDSSettings, error) {
	read := func() (*franchiseapi.KDSSettings, error) {
		if scope.Type == franchiseapi.LocationCentralKitchen {
			return scanKDSSettings(s.pool.QueryRow(ctx, kdsSettingsSelect+` WHERE central_kitchen_id = $1`, scope.CentralKitchenID))
		}
		return scanKDSSettings(s.pool.QueryRow(ctx, kdsSettingsSelect+` WHERE branch_id = $1`, scope.BranchID))
	}
	k, err := read()
	if !errors.Is(err, shared.ErrNotFound) {
		return k, err
	}
	if err := s.EnsureKDSSettings(ctx, scope); err != nil {
		return nil, err
	}
	return read()
}

// GetKDSSettings reads one settings row by id.
func (s *Repo) GetKDSSettings(ctx context.Context, id string) (*franchiseapi.KDSSettings, error) {
	return scanKDSSettings(s.pool.QueryRow(ctx, kdsSettingsSelect+` WHERE id = $1`, id))
}

// UpdateKDSSettings changes only the fields the patch carries.
func (s *Repo) UpdateKDSSettings(ctx context.Context, id string, p franchiseapi.KDSSettingsPatch) (*franchiseapi.KDSSettings, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE kds_settings SET
			kds_enabled               = COALESCE($2, kds_enabled),
			show_pickup               = COALESCE($3, show_pickup),
			show_delivery             = COALESCE($4, show_delivery),
			show_takeaway             = COALESCE($5, show_takeaway),
			show_orders_based_on_time = COALESCE($6, show_orders_based_on_time),
			order_appear_minutes      = COALESCE($7, order_appear_minutes),
			play_sound_on_new_ticket  = COALESCE($8, play_sound_on_new_ticket),
			ticket_layout             = COALESCE($9, ticket_layout),
			ticket_columns            = COALESCE($10, ticket_columns),
			header_colors_enabled     = COALESCE($11, header_colors_enabled),
			pickup_color              = COALESCE($12, pickup_color),
			delivery_color            = COALESCE($13, delivery_color),
			takeaway_color            = COALESCE($14, takeaway_color),
			dark_theme                = COALESCE($15, dark_theme),
			calendar_low_from         = COALESCE($16, calendar_low_from),
			calendar_low_to           = COALESCE($17, calendar_low_to),
			calendar_medium_from      = COALESCE($18, calendar_medium_from),
			calendar_medium_to        = COALESCE($19, calendar_medium_to),
			calendar_high_from        = COALESCE($20, calendar_high_from),
			updated_at                = NOW()
		 WHERE id = $1`,
		id, p.KDSEnabled, p.ShowPickup, p.ShowDelivery, p.ShowTakeaway, p.ShowOrdersBasedOnTime,
		p.OrderAppearMinutes, p.PlaySoundOnNewTicket, p.TicketLayout, p.TicketColumns, p.HeaderColorsEnabled,
		p.PickupColor, p.DeliveryColor, p.TakeawayColor, p.DarkTheme,
		p.CalendarLowFrom, p.CalendarLowTo, p.CalendarMediumFrom, p.CalendarMediumTo, p.CalendarHighFrom)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, shared.ErrNotFound
	}
	return s.GetKDSSettings(ctx, id)
}
