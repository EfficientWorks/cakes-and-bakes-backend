package api

import (
	"bakehouse/services/orders/ordersapi"
)

// orderSummaryDTO is the lighter shape for list views (ADM-01, account/orders).
type orderSummaryDTOType struct {
	ID             string  `json:"id"`
	Reference      string  `json:"reference"`
	BillNo         *int    `json:"billNo"`
	OrderNo        *int    `json:"orderNo"`
	BranchID       string  `json:"branchId"`
	Status         string  `json:"status"`
	FulfilmentType string  `json:"fulfilmentType"`
	RecipientName  string  `json:"recipientName"`
	PurchaserName  string  `json:"purchaserName"`
	PurchaserPhone *string `json:"purchaserPhone"`
	ScheduledDate  string  `json:"scheduledDate"`
	SlotStart      string  `json:"slotStart"`
	SlotEnd        string  `json:"slotEnd"`
	TotalCents     int64   `json:"totalCents"`
	// What this order counts towards the list's total: 0 for a cancelled,
	// fully returned or refunded order, otherwise totalCents (already net of
	// any items returned). Summing this over the rows gives stats.totalPrice.
	NetTotalCents      int64 `json:"netTotalCents"`
	ServiceChargeCents int64 `json:"serviceChargeCents"`
	TipCents           int64 `json:"tipCents"`
	// Units handed back and what they were sold for; zero with no returns.
	ReturnedQuantity   int   `json:"returnedQuantity"`
	ReturnedTotalCents int64 `json:"returnedTotalCents"`
	// Discount, tax and refund over every return on the order.
	ReturnSummary returnChargesDTOType `json:"returnSummary"`
	// Payment state on the list, not just the detail: a cash-on-delivery
	// order still owing money is the one an operator most needs to spot
	// while scanning, and making them open each order to find out defeats
	// the purpose of the list.
	PaymentStatus string             `json:"paymentStatus"`
	PaymentMethod string             `json:"paymentMethod"`
	PaymentType   string             `json:"paymentType"`
	Channel       string             `json:"channel"`
	ItemCount     int                `json:"itemCount"`
	Items         []orderItemDTOType `json:"items"`
	PlacedAt      string             `json:"placedAt"`
	// The coupon used, on the summary as well as the detail, so an export of
	// the order list can say which offer a discount came from without
	// fetching every order one at a time.
	DiscountCode    *string              `json:"discountCode"`
	CustomerID      *string              `json:"customerId"`
	Customer        customerOrderDTOType `json:"customer"`
	BillIDs         []string             `json:"billIds"`
	PendingBillIDs  []pendingBillDTOType `json:"pendingBillIds"`
	TotalBillsCount int                  `json:"totalBillsCount"`
	SplitMode       *string              `json:"splitMode"`

	BillerID          *string `json:"billerId"`
	BillerName        *string `json:"billerName"`
	UpdatedBillerID   *string `json:"updatedBillerId"`
	UpdatedBillerName *string `json:"updatedBillerName"`

	StartTime      string  `json:"startTime"`
	EndTime        string  `json:"endTime"`
	TotalDuration  *int    `json:"totalDuration"`
	SettledBy      *string `json:"settledBy"`
	SettledByName  *string `json:"settledByName"`
	SettledByID    *string `json:"settledById"`
	ReturnedBy     *string `json:"returnedBy"`
	ReturnedByName *string `json:"returnedByName"`
	ReturnedByID   *string `json:"returnedById"`
	// When it was fully paid, and who cancelled it, when and why.
	settleCancelDTO

	GuestCustomerName     *string `json:"guestCustomerName" example:"Jane Doe"`
	GuestCustomerMobile   *string `json:"guestCustomerMobile" example:"+1234567890"`
	GuestCustomerAddress  *string `json:"guestCustomerAddress" example:"123 Flower Street"`
	GuestCustomerEmail    *string `json:"guestCustomerEmail" example:"guest@example.com"`
	GuestCustomerCity     *string `json:"guestCustomerCity" example:"Edison"`
	GuestCustomerPostcode *string `json:"guestCustomerPostcode" example:"08817"`
	OrderDeliverDate      *string `json:"orderDeliverDate" example:"2026-09-07"`
	OrderDeliverTime      *string `json:"orderDeliverTime" example:"14:30:00"`

	// KDS
	KotStatus           string `json:"kotStatus" example:"queue"`
	IsAmended           bool   `json:"isAmended"`
	AmendedAcknowledged bool   `json:"amendedAcknowledged"`
}

// returnedTotals is what came back on an order: units, and what those units
// were sold for — the same per-line rate the return itself refunded
// (unit price × returned quantity, see store.ReturnPOSOrder).
func returnedTotals(items []ordersapi.OrderItem) (qty int, cents int64) {
	for _, i := range items {
		if i.ReturnedQuantity > 0 {
			qty += i.ReturnedQuantity
			cents += int64(i.ReturnedQuantity) * i.UnitPriceCents
		}
	}
	return qty, cents
}

func orderSummaryDTO(o *ordersapi.Order) orderSummaryDTOType {
	// ItemCount comes from the list query's subquery (o.ItemCount); Items
	// is intentionally left empty for list views, so len(o.Items) would
	// always read 0 here — see the ItemCount field doc comment on ordersapi.Order.
	count := o.ItemCount
	if count == 0 && len(o.Items) > 0 {
		count = len(o.Items)
	}
	billIDs := o.BillIDs
	if billIDs == nil {
		billIDs = []string{}
	}
	items := make([]orderItemDTOType, 0, len(o.Items))
	for i := range o.Items {
		items = append(items, orderItemDTO(&o.Items[i]))
	}
	returnedQty, returnedCents := returnedTotals(o.Items)
	return orderSummaryDTOType{
		ID: o.ID, Reference: o.Reference, BillNo: o.BillNo, OrderNo: o.BillNo, BranchID: o.BranchID, Status: o.Status, FulfilmentType: o.FulfilmentType,
		RecipientName: o.RecipientFirstName + " " + o.RecipientLastName,
		PurchaserName: o.PurchaserFirstName + " " + o.PurchaserLastName, PurchaserPhone: o.PurchaserPhone,
		ScheduledDate: o.ScheduledDate.Format(dateFmt), SlotStart: o.SlotStart, SlotEnd: o.SlotEnd,
		TotalCents: o.TotalCents, NetTotalCents: netTotalCents(o), ServiceChargeCents: o.ServiceChargeCents, TipCents: o.TipCents,
		ReturnedQuantity: returnedQty, ReturnedTotalCents: returnedCents,
		ReturnSummary: orderReturnSummaryDTO(o),
		PaymentStatus: o.PaymentStatus, PaymentMethod: o.PaymentMethod,
		PaymentType: o.PaymentMethod, Channel: o.Channel, ItemCount: count, Items: items, PlacedAt: o.CreatedAt.Format(rfc3339),
		DiscountCode:          o.DiscountCode,
		CustomerID:            o.CustomerID,
		Customer:              buildCustomerDTO(o),
		BillIDs:               billIDs,
		PendingBillIDs:        pendingBillDTOList(o.PendingBillIDs),
		TotalBillsCount:       o.TotalBillsCount,
		SplitMode:             o.SplitMode,
		BillerID:              o.BillerID,
		BillerName:            o.BillerName,
		UpdatedBillerID:       o.UpdatedBillerID,
		UpdatedBillerName:     o.UpdatedBillerName,
		StartTime:             orderStartTime(o),
		EndTime:               orderEndTime(o),
		TotalDuration:         totalDuration(o),
		SettledBy:             settledDisplay(o),
		SettledByName:         o.SettledByName,
		SettledByID:           o.SettledBy,
		ReturnedBy:            returnedDisplay(o),
		ReturnedByName:        o.ReturnedByName,
		ReturnedByID:          o.ReturnedBy,
		settleCancelDTO:       settleCancel(o),
		GuestCustomerName:     o.GuestCustomerName,
		GuestCustomerMobile:   o.GuestCustomerMobile,
		GuestCustomerAddress:  o.GuestCustomerAddress,
		GuestCustomerEmail:    o.GuestCustomerEmail,
		GuestCustomerCity:     o.GuestCustomerCity,
		GuestCustomerPostcode: o.GuestCustomerPostcode,
		OrderDeliverDate:      o.OrderDeliverDate,
		OrderDeliverTime:      o.OrderDeliverTime,
		KotStatus:             orDefaultStr(o.KOTStatus, ordersapi.KOTQueue),
		IsAmended:             o.IsAmended,
		AmendedAcknowledged:   o.AmendedAcknowledged,
	}
}

// --- Public tracking DTO (SF-32): deliberately minimal ------------------------

type trackingDTOType struct {
	Reference      string                 `json:"reference"`
	Status         string                 `json:"status"`
	Recipient      string                 `json:"recipient"`
	FulfilmentType string                 `json:"fulfilmentType"`
	ScheduledDate  string                 `json:"scheduledDate"`
	SlotStart      string                 `json:"slotStart"`
	SlotEnd        string                 `json:"slotEnd"`
	Timeline       []timelineEventDTOType `json:"timeline"`
}

func trackingDTO(o *ordersapi.Order) trackingDTOType {
	out := trackingDTOType{
		Reference: o.Reference, Status: o.Status, Recipient: o.RecipientFirstName + " " + initialOf(o.RecipientLastName),
		FulfilmentType: o.FulfilmentType, ScheduledDate: o.ScheduledDate.Format(dateFmt), SlotStart: o.SlotStart, SlotEnd: o.SlotEnd,
	}
	for _, e := range o.Timeline {
		out.Timeline = append(out.Timeline, timelineEventDTOType{Status: e.Status, At: e.CreatedAt.Format(rfc3339)})
	}
	return out
}
