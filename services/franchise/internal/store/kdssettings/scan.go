package kdssettings

import (
	"errors"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/franchise/franchiseapi"
	"bakehouse/services/franchise/internal/store/shared"
)

func scanKDSSettings(row pgx.Row) (*franchiseapi.KDSSettings, error) {
	var k franchiseapi.KDSSettings
	err := row.Scan(&k.ID, &k.ScopeType, &k.BranchID, &k.CentralKitchenID, &k.KDSEnabled,
		&k.ShowPickup, &k.ShowDelivery, &k.ShowTakeaway, &k.ShowOrdersBasedOnTime, &k.OrderAppearMinutes,
		&k.PlaySoundOnNewTicket, &k.TicketLayout, &k.TicketColumns, &k.HeaderColorsEnabled,
		&k.PickupColor, &k.DeliveryColor, &k.TakeawayColor, &k.DarkTheme,
		&k.CalendarLowFrom, &k.CalendarLowTo, &k.CalendarMediumFrom, &k.CalendarMediumTo, &k.CalendarHighFrom,
		&k.CreatedAt, &k.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.ErrNotFound
	}
	return &k, err
}
