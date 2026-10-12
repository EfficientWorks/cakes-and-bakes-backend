package branch

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/franchise/franchiseapi"
	"bakehouse/services/franchise/internal/store/shared"
)

func scanBranch(row pgx.Row) (*franchiseapi.Branch, error) {
	var b franchiseapi.Branch
	var hours, zone, pricing, pickupHours, deliveryHours []byte
	err := row.Scan(&b.ID, &b.Name, &b.Code, &b.Type, &b.OwnershipType, &b.Street, &b.City, &b.State, &b.Postcode, &b.Country,
		&b.Latitude, &b.Longitude, &b.Phone, &b.Email, &b.Timezone, &hours, &b.FulfilmentTypes, &zone,
		&b.DeliveryFeeCents, &b.MinOrderCents, &b.TaxName, &b.TaxRatePct,
		&b.AcceptsCashOnDelivery, &b.CODMaxOrderCents, &pricing,
		&b.CardProviders, &pickupHours, &deliveryHours, &b.SameFulfilmentHours, &b.Status, &b.EmergencyClosedOn, &b.BusinessID, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, shared.ErrNotFound
		}
		return nil, fmt.Errorf("scan branch: %w", err)
	}
	_ = json.Unmarshal(hours, &b.OpeningHours)
	if len(pickupHours) > 0 {
		_ = json.Unmarshal(pickupHours, &b.PickupHours)
	}
	if len(deliveryHours) > 0 {
		_ = json.Unmarshal(deliveryHours, &b.DeliveryHours)
	}
	_ = json.Unmarshal(zone, &b.DeliveryZone)
	_ = json.Unmarshal(pricing, &b.DeliveryPricing)
	return &b, nil
}
