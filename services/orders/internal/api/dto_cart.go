package api

import (
	"bakehouse/services/orders/ordersapi"
)

// --- Cart --------------------------------------------------------------------

type cartItemDTOType struct {
	ID          string `json:"cartItemId"`
	ProductID   string `json:"productId"`
	ProductName string `json:"productName"`
	// ProductSlug is what the cart screen's "Edit" link routes to —
	// /products/{slug}. Null only if the product has since been deleted.
	ProductSlug  *string           `json:"productSlug"`
	ProductImage *string           `json:"productImage"`
	VariantID    *string           `json:"variantId"`
	VariantName  *string           `json:"variantName"`
	Flavours     []modifierDTOType `json:"flavours"`
	Addons       []modifierDTOType `json:"addons"`
	Presentation *modifierDTOType  `json:"presentation"`
	WrapColour   *modifierDTOType  `json:"wrapColour"`
	Quantity     int               `json:"quantity"`
	UnitPrice    int64             `json:"unitPrice"`
	LineTotal    int64             `json:"lineTotal"`
	IsGift       bool              `json:"isGift"`
	Available    bool              `json:"available"`
	// Set when a seasonal tag's restricted dates rule this line out for the
	// cart's chosen slot: "Unavailable Nov 25", available again from a date.
	Restriction *itemRestrictionDTOType `json:"restriction"`
	Recipient   *recipientDTOType       `json:"recipient,omitempty"`
	GiftMessage *string                 `json:"giftMessage"`
}

type recipientDTOType struct {
	FirstName *string `json:"firstName"`
	LastName  *string `json:"lastName"`
}

func cartItemDTO(i *ordersapi.CartItem) cartItemDTOType {
	out := cartItemDTOType{
		ID: i.ID, ProductID: i.ProductID, ProductName: i.ProductName,
		ProductSlug: i.ProductSlug, ProductImage: i.ProductImageURL,
		VariantID: i.VariantID, VariantName: i.VariantName, Flavours: modDTOList(i.SelectedFlavours), Addons: modDTOList(i.SelectedAddons),
		Presentation: modDTOPtr(i.SelectedPresentation), WrapColour: modDTOPtr(i.SelectedWrapColour),
		Quantity: i.Quantity, UnitPrice: i.UnitPriceCents, LineTotal: i.LineTotalCents(),
		IsGift: i.IsGift, Available: i.Available, GiftMessage: i.GiftMessage,
	}
	if i.RecipientFirstName != nil || i.RecipientLastName != nil {
		out.Recipient = &recipientDTOType{FirstName: i.RecipientFirstName, LastName: i.RecipientLastName}
	}
	if r := i.Restriction; r != nil {
		out.Restriction = &itemRestrictionDTOType{TagID: r.TagID, TagName: r.TagName, WindowFrom: r.WindowFrom,
			WindowTo: r.WindowTo, AvailableFrom: r.AvailableFrom, Message: r.Message}
	}
	return out
}

type itemRestrictionDTOType struct {
	TagID         string `json:"tagId"`
	TagName       string `json:"tagName"`
	WindowFrom    string `json:"windowFrom" example:"2026-11-24T00:30:00"`
	WindowTo      string `json:"windowTo" example:"2026-11-27T13:00:00"`
	AvailableFrom string `json:"availableFrom" example:"2026-11-28"`
	Message       string `json:"message"`
}

type cartDTOType struct {
	CartID          string              `json:"cartId"`
	BranchID        string              `json:"branchId"`
	FulfilmentType  string              `json:"fulfilmentType"`
	DeliveryAddress *addressDTOType     `json:"deliveryAddress,omitempty"`
	ScheduledDate   *string             `json:"scheduledDate"`
	SlotStart       *string             `json:"slotStart"`
	SlotEnd         *string             `json:"slotEnd"`
	Items           []cartItemDTOType   `json:"items"`
	Summary         priceSummaryDTOType `json:"summary"`
	// The applied discount code, so the basket can show it as applied with a
	// way to remove it rather than leaving an empty box over a discounted
	// total. Null when none is applied.
	DiscountCode *string `json:"discountCode"`
}

type addressDTOType struct {
	Street   *string `json:"street"`
	City     *string `json:"city"`
	State    *string `json:"state"`
	Postcode *string `json:"postcode"`
}

func cartDTO(c *ordersapi.Cart, summary *ordersapi.PriceSummary) cartDTOType {
	out := cartDTOType{
		CartID: c.ID, BranchID: c.BranchID, FulfilmentType: c.FulfilmentType,
		Summary: priceSummaryDTO(summary), DiscountCode: c.DiscountCode,
	}
	if c.DeliveryStreet != nil {
		out.DeliveryAddress = &addressDTOType{Street: c.DeliveryStreet, City: c.DeliveryCity, State: c.DeliveryState, Postcode: c.DeliveryPostcode}
	}
	if c.ScheduledDate != nil {
		d := c.ScheduledDate.Format(dateFmt)
		out.ScheduledDate = &d
	}
	out.SlotStart = c.SlotStart
	out.SlotEnd = c.SlotEnd
	for _, i := range c.Items {
		out.Items = append(out.Items, cartItemDTO(&i))
	}
	if out.Items == nil {
		out.Items = []cartItemDTOType{}
	}
	return out
}

// --- Slots ---------------------------------------------------------------------

type slotDTOType struct {
	Start     string `json:"start"`
	End       string `json:"end"`
	Remaining int    `json:"remaining"`
}

type daySlotsDTOType struct {
	Date  string        `json:"date"`
	Label string        `json:"label"`
	Slots []slotDTOType `json:"slots"`
}

func daySlotsDTO(d *ordersapi.DaySlots) daySlotsDTOType {
	out := daySlotsDTOType{Date: d.Date, Label: d.Label}
	for _, s := range d.Slots {
		out.Slots = append(out.Slots, slotDTOType{Start: s.SlotStart, End: s.SlotEnd, Remaining: s.Remaining()})
	}
	return out
}

type scheduleConfigDTOType struct {
	SlotDurationMins   int    `json:"slotDurationMins"`
	PickupCutoffTime   string `json:"pickupCutoffTime"`
	DeliveryCutoffTime string `json:"deliveryCutoffTime"`
	MaxOrdersPerSlot   int    `json:"maxOrdersPerSlot"`
	DefaultLeadHours   int    `json:"defaultLeadHours"`
	AdvanceDaysMax     int    `json:"advanceDaysMax"`
}

func scheduleConfigDTO(c *ordersapi.ScheduleConfig) scheduleConfigDTOType {
	return scheduleConfigDTOType{
		SlotDurationMins: c.SlotDurationMins, PickupCutoffTime: c.PickupCutoffTime, DeliveryCutoffTime: c.DeliveryCutoffTime,
		MaxOrdersPerSlot: c.MaxOrdersPerSlot, DefaultLeadHours: c.DefaultLeadHours, AdvanceDaysMax: c.AdvanceDaysMax,
	}
}
