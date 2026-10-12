package order

import (
	"errors"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/orders/ordersapi"
)

// ScanTargets is the scan destination list for SelectSQL, in column
// order.
//
// It exists so the two scans cannot drift from the one query they share.
// orderListSelectWithCount is derived from SelectSQL by splicing in a count,
// but the scans used to be written out separately — so adding a column to
// SelectSQL silently broke the list scan, and every admin order list
// answered INTERNAL_ERROR with nothing to say which column was wrong. Adding a
// column now means editing this list, and both scans follow.
func ScanTargets(o *ordersapi.Order) []any {
	return []any{
		&o.ID, &o.Reference, &o.BillNo, &o.BranchID, &o.CustomerID, &o.Channel, &o.FulfilmentType,
		&o.RecipientFirstName, &o.RecipientLastName, &o.RecipientPhone, &o.RecipientEmail, &o.GiftMessage, &o.GiftReason, &o.IsGift,
		&o.PurchaserFirstName, &o.PurchaserLastName, &o.PurchaserEmail, &o.PurchaserPhone,
		&o.DeliveryStreet, &o.DeliveryCity, &o.DeliveryState, &o.DeliveryPostcode, &o.DeliveryInstructions,
		&o.BusinessName, &o.DeliveryAddressType,
		&o.PhotoOnDelivery, &o.SurpriseDelivery,
		&o.ScheduledDate, &o.SlotStart, &o.SlotEnd, &o.SlotCapacityID,
		&o.SubtotalCents, &o.DeliveryFeeCents, &o.TaxCents, &o.TipCents, &o.DiscountCents, &o.DiscountCode,
		&o.DiscountPct, &o.TaxPct, &o.ServiceChargeCents, &o.TotalCents, &o.Currency,
		&o.Status, &o.PaymentStatus, &o.PaymentMethod, &o.CashCollectedAt, &o.CashCollectedBy,
		&o.ProviderPaymentID, &o.IdempotencyKey, &o.MarketingOptIn, &o.Notes,
		&o.SettledBy, &o.ReturnedBy, &o.TotalDurationSecs, &o.StartTime, &o.EndTime,
		&o.SettledByName,
		&o.ReturnedByName,
		&o.BillerID, &o.UpdatedBillerID,
		&o.BillerName,
		&o.UpdatedBillerName,
		&o.SplitMode,
		&o.GuestCustomerName, &o.GuestCustomerMobile, &o.GuestCustomerAddress, &o.GuestCustomerEmail,
		&o.GuestCustomerCity, &o.GuestCustomerPostcode, &o.GuestCustomerStreet, &o.GuestCustomerState,
		&o.OrderDeliverDate, &o.OrderDeliverTime,
		&o.CreatedAt, &o.UpdatedAt,
		&o.KOTStatus, &o.KOTStartedAt, &o.KOTReadyAt, &o.KOTCompletedAt,
		&o.IsAmended, &o.AmendedAt, &o.AmendedAcknowledged, &o.AmendedAcknowledgedAt,
		&o.ReturnAmountCents, &o.ReturnDiscountPct, &o.ReturnDiscountCents,
		&o.ReturnTaxPct, &o.ReturnTaxCents, &o.ReturnTotalCents,
		&o.SettledAt, &o.Cancellation,
	}
}

func scanOrder(row pgx.Row) (*ordersapi.Order, error) {
	var o ordersapi.Order
	err := row.Scan(ScanTargets(&o)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ordersapi.ErrNotFound
	}
	return &o, err
}

// scanOrderWithCount is scanOrder plus the item count, which
// orderListSelectWithCount appends as the last column.
func scanOrderWithCount(row pgx.Row) (*ordersapi.Order, error) {
	var o ordersapi.Order
	err := row.Scan(append(ScanTargets(&o), &o.ItemCount)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ordersapi.ErrNotFound
	}
	return &o, err
}
