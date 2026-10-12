package order

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/jackc/pgx/v5"

	"bakehouse/pkg/timex"
	"bakehouse/services/orders/internal/store/recipient"
	"bakehouse/services/orders/internal/store/shared"
	"bakehouse/services/orders/internal/store/slot"
	"bakehouse/services/orders/ordersapi"
)

// CreateOrderTx creates an order, its items, reserves slot capacity, and
// records the initial status event — all inside one transaction (ORD-01,
// SCH-04). idempotencyKey is unique; a replay with the same key returns the
// existing order id and ordersapi.ErrIdempotentReplay so the handler can fetch and
// return the original response instead of creating a duplicate order.
func (s *Repo) CreateOrderTx(ctx context.Context, o ordersapi.Order, items []ordersapi.OrderItem) (string, error) {
	if existingID, err := s.findOrderIDByIdempotencyKey(ctx, o.IdempotencyKey); err == nil {
		return existingID, ordersapi.ErrIdempotentReplay
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	if o.SlotCapacityID != nil {
		if err := slot.ReserveSlot(ctx, tx, *o.SlotCapacityID); err != nil {
			return "", err
		}
	}
	billNo, err := NextBillNo(ctx, tx, o.BranchID)
	if err != nil {
		return "", err
	}
	const q = `
		INSERT INTO orders (reference, branch_id, customer_id, channel, fulfilment_type,
			recipient_first_name, recipient_last_name, recipient_phone, recipient_email, gift_message, gift_reason, is_gift,
			purchaser_first_name, purchaser_last_name, purchaser_email, purchaser_phone,
			delivery_street, delivery_city, delivery_state, delivery_postcode, delivery_instructions,
			scheduled_date, slot_start, slot_end, slot_capacity_id,
			subtotal_cents, delivery_fee_cents, tax_cents, tip_cents, discount_cents, discount_code, total_cents, currency,
			status, payment_status, payment_method, provider_payment_id, idempotency_key, marketing_opt_in,
			photo_on_delivery, surprise_delivery, bill_no, business_name, delivery_address_type, start_time, end_time, notes)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,
			$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34,$35,$36,$37,$38,$39,$40,$41,$42,$43,$44,
			COALESCE($45::timestamptz, NOW()),
			-- An end time only means something on an order that arrives closed.
			CASE WHEN $34 IN ('settled', 'fulfilled', 'cancelled', 'returned') THEN $46::timestamptz ELSE NULL END,
			$47)
		RETURNING id`
	var orderID string
	err = tx.QueryRow(ctx, q, o.Reference, o.BranchID, o.CustomerID, o.Channel, o.FulfilmentType,
		o.RecipientFirstName, o.RecipientLastName, o.RecipientPhone, o.RecipientEmail, o.GiftMessage, o.GiftReason, o.IsGift,
		o.PurchaserFirstName, o.PurchaserLastName, o.PurchaserEmail, o.PurchaserPhone,
		o.DeliveryStreet, o.DeliveryCity, o.DeliveryState, o.DeliveryPostcode, o.DeliveryInstructions,
		o.ScheduledDate, o.SlotStart, o.SlotEnd, o.SlotCapacityID,
		o.SubtotalCents, o.DeliveryFeeCents, o.TaxCents, o.TipCents, o.DiscountCents, o.DiscountCode, o.TotalCents, o.Currency,
		o.Status, o.PaymentStatus, orDefaultMethod(o.PaymentMethod), o.ProviderPaymentID, o.IdempotencyKey, o.MarketingOptIn,
		o.PhotoOnDelivery, o.SurpriseDelivery, billNo, o.BusinessName, o.DeliveryAddressType, timex.EventTimeFrom(ctx), o.EndTime, o.Notes).Scan(&orderID)
	if err != nil {
		return "", fmt.Errorf("insert order: %w", err)
	}

	if o.PurchaserEmail != "" && o.RecipientEmail != nil && strings.TrimSpace(*o.RecipientEmail) != "" {
		if err := recipient.UpsertStorefrontRecipient(ctx, tx, ordersapi.StorefrontRecipient{
			PurchaserEmail: o.PurchaserEmail, CustomerID: o.CustomerID, RecipientEmail: *o.RecipientEmail,
			FirstName: o.RecipientFirstName, LastName: o.RecipientLastName, Phone: o.RecipientPhone,
			BusinessName: o.BusinessName, DeliveryAddressType: o.DeliveryAddressType,
			DeliveryInstructions: o.DeliveryInstructions, DeliveryPostcode: o.DeliveryPostcode,
			DeliveryState: o.DeliveryState, DeliveryCity: o.DeliveryCity, DeliveryStreet: o.DeliveryStreet,
		}); err != nil {
			return "", fmt.Errorf("upsert storefront recipient: %w", err)
		}
	}

	for _, item := range items {
		flavours, _ := json.Marshal(item.SelectedFlavours)
		addons, _ := json.Marshal(item.SelectedAddons)
		pres, _ := json.Marshal(item.SelectedPresentation)
		wrap, _ := json.Marshal(item.SelectedWrapColour)
		if _, err := tx.Exec(ctx, `
			INSERT INTO order_items (order_id, product_id, product_name, variant_id, variant_name, quantity,
				unit_price_cents, line_total_cents, selected_flavours, selected_addons, selected_presentation, selected_wrap_colour,comments,
				gift_message, gift_reason, send_kot_time)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
			orderID, shared.NilIfEmptyStr(item.ProductID), item.ProductName, item.VariantID, item.VariantName, item.Quantity,
			item.UnitPriceCents, item.LineTotalCents, flavours, addons, pres, wrap, item.Comments,
			shared.NilIfEmptyPtr(item.GiftMessage), shared.NilIfEmptyPtr(item.GiftReason), item.SendKotTime); err != nil {
			return "", fmt.Errorf("insert order item: %w", err)
		}
	}

	if _, err := tx.Exec(ctx, `INSERT INTO order_status_events (order_id, status, actor_type, note, created_at)
		VALUES ($1,$2,'system','Order placed', COALESCE($3::timestamptz, NOW()))`, orderID, o.Status, timex.EventTimeFrom(ctx)); err != nil {
		return "", err
	}

	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return orderID, nil
}

// NewOrderReference generates a short, human-readable order code, e.g.
// "EZYODR4F82C1" — matches the shape used throughout the BRD and Figma.
func NewOrderReference() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0/O/1/I ambiguity
	b := make([]byte, 8)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		b[i] = alphabet[n.Int64()]
	}
	return "BB" + string(b), nil
}

func NextBillNo(ctx context.Context, tx pgx.Tx, branchID string) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `
		INSERT INTO branch_bill_counters (branch_id, last_bill_no)
		VALUES ($1, 1)
		ON CONFLICT (branch_id) DO UPDATE
		   SET last_bill_no = branch_bill_counters.last_bill_no + 1,
		       updated_at   = NOW()
		RETURNING last_bill_no`, branchID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("next bill no: %w", err)
	}
	return n, nil
}

// orDefaultMethod keeps the column's CHECK constraint satisfied for callers
// that predate cash on delivery and leave PaymentMethod unset — every one of
// those is a card order, which is what the column defaults to anyway.
func orDefaultMethod(method string) string {
	if method == "" {
		return "card"
	}
	return method
}
