package api

import "bakehouse/services/franchise/franchiseapi"

const rfc3339 = "2006-01-02T15:04:05Z07:00"

type branchDTOType struct {
	BusinessID  *string `json:"businessId"`
	ID          string  `json:"id"`
	FranchiseID string  `json:"-"`
	Name        string  `json:"name"`
	Code        string  `json:"code"`
	// Same value as ownershipType; kept so existing consoles that read
	// "type" keep working. New code should read ownershipType.
	Type             string                     `json:"type"`
	OwnershipType    string                     `json:"ownershipType"`
	IsCompanyOwned   bool                       `json:"-"`
	BranchType       string                     `json:"-"`
	Street           string                     `json:"street"`
	City             string                     `json:"city"`
	State            string                     `json:"state"`
	Postcode         string                     `json:"postcode"`
	Country          string                     `json:"country"`
	Latitude         *float64                   `json:"latitude"`
	Longitude        *float64                   `json:"longitude"`
	Phone            *string                    `json:"phone"`
	Email            *string                    `json:"email"`
	Timezone         string                     `json:"timezone"`
	OpeningHours     []franchiseapi.OpeningHour `json:"openingHours"`
	FulfilmentTypes  []string                   `json:"fulfilmentTypes"`
	DeliveryZone     franchiseapi.DeliveryZone  `json:"deliveryZone"`
	DeliveryFeeCents int64                      `json:"deliveryFeeCents"`
	MinOrderCents    int64                      `json:"minOrderCents"`
	TaxName          *string                    `json:"taxName"`
	TaxRatePct       *float64                   `json:"taxRatePct"`
	// Cash on delivery, surfaced on the storefront too: checkout can only
	// offer the option for a branch that actually takes cash.
	AcceptsCashOnDelivery bool   `json:"acceptsCashOnDelivery"`
	CODMaxOrderCents      *int64 `json:"codMaxOrderCents"`
	// Card providers switched on for this store ("stripe"). The
	// storefront and POS offer only those also configured on the server.
	CardProviders []string `json:"cardProviders"`
	// Pickup and delivery hours: each day closed or a list of ranges. Always
	// filled — a store that never set them shows its opening hours.
	PickupHours         []franchiseapi.DayHours `json:"pickupHours"`
	DeliveryHours       []franchiseapi.DayHours `json:"deliveryHours"`
	SameFulfilmentHours bool                    `json:"sameFulfilmentHours"`
	// How the store charges for delivery (region, charge type, fees). For a
	// store saved before pricing existed: its ZIP list with the flat fee.
	DeliveryPricing franchiseapi.DeliveryPricing `json:"deliveryPricing"`
	// The fee for one address, on delivery-check responses only.
	DeliveryQuote *franchiseapi.DeliveryQuote `json:"deliveryQuote,omitempty"`
	Status        string                      `json:"status"`
	CreatedAt     string                      `json:"createdAt"`
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func branchDTO(b *franchiseapi.Branch) branchDTOType {
	ownership := b.OwnershipType
	if ownership == "" {
		ownership = franchiseapi.OwnershipFranchise
	}
	return branchDTOType{
		ID: b.ID, Name: b.Name, Code: b.Code,
		Type:          ownership,
		OwnershipType: ownership,
		BranchType:    b.Type,
		Street:        b.Street, City: b.City, State: b.State, Postcode: b.Postcode, Country: b.Country,
		Latitude: b.Latitude, Longitude: b.Longitude, Phone: b.Phone, Email: b.Email,
		Timezone: b.Timezone, OpeningHours: b.OpeningHours,
		FulfilmentTypes: b.FulfilmentTypes, DeliveryZone: b.DeliveryZone,
		DeliveryFeeCents: b.DeliveryFeeCents, MinOrderCents: b.MinOrderCents,
		TaxName: b.TaxName, TaxRatePct: b.TaxRatePct,
		AcceptsCashOnDelivery: b.AcceptsCashOnDelivery, CODMaxOrderCents: b.CODMaxOrderCents,
		CardProviders:       nonNilStrings(b.CardProviders),
		PickupHours:         b.HoursFor("pickup"),
		DeliveryHours:       b.HoursFor("delivery"),
		SameFulfilmentHours: b.SameFulfilmentHours,
		DeliveryPricing:     b.Pricing(),
		Status:              b.Status, BusinessID: b.BusinessID, CreatedAt: b.CreatedAt.Format(rfc3339),
	}
}

type branchWithDistanceDTOType struct {
	branchDTOType
	DistanceMiles *float64 `json:"distanceMiles"`
}

func branchWithDistanceDTO(b *franchiseapi.BranchWithDistance) branchWithDistanceDTOType {
	return branchWithDistanceDTOType{branchDTOType: branchDTO(&b.Branch), DistanceMiles: roundMiles(b.DistanceMiles)}
}

// roundMiles keeps a distance to two decimals for display.
func roundMiles(d *float64) *float64 {
	if d == nil {
		return nil
	}
	v := float64(int64(*d*100+0.5)) / 100
	return &v
}

type exceptionDTOType struct {
	ID        string  `json:"id"`
	Date      string  `json:"date"`
	Reason    string  `json:"reason"`
	IsClosed  bool    `json:"isClosed"`
	OpenTime  *string `json:"openTime"`
	CloseTime *string `json:"closeTime"`
}

func exceptionDTO(e *franchiseapi.BranchException) exceptionDTOType {
	return exceptionDTOType{
		ID: e.ID, Date: e.Date.Format("2006-01-02"), Reason: e.Reason,
		IsClosed: e.IsClosed, OpenTime: e.OpenTime, CloseTime: e.CloseTime,
	}
}

type governanceRuleDTOType struct {
	FieldKey    string `json:"fieldKey"`
	Level       string `json:"level"`
	Description string `json:"description"`
}
