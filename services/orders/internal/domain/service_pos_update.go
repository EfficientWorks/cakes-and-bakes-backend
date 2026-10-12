package domain

import (
	"bakehouse/pkg/realtime"
	"bakehouse/pkg/timex"
	"bakehouse/services/orders/internal/store"
	"context"
	"errors"
	"fmt"
	"strings"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/tenant"
	"bakehouse/services/orders/ordersapi"
)

/*
 * assertKitchenAllowsEdit refuses changes to what the kitchen is already
 * making (KDS flow). A line in preparation or ready cannot be cancelled or
 * have its quantity changed from the POS/admin; a new line is always fine
 * (it joins the queue). Orders the kitchen has not touched are unaffected —
 * every line starts in the queue.
 */
func assertKitchenAllowsEdit(order *ordersapi.Order, items []ordersapi.POSItem) error {
	current := map[string]*ordersapi.OrderItem{}
	for i := range order.Items {
		current[order.Items[i].ID] = &order.Items[i]
	}
	for i, it := range items {
		if it.ID == "" {
			continue
		}
		cur, ok := current[it.ID]
		if !ok {
			continue
		}
		if cur.KOTStatus != ordersapi.ItemKOTPreparing && cur.KOTStatus != ordersapi.ItemKOTReady {
			continue
		}
		cancelling := it.Status == ordersapi.ItemCancel || it.Status == ordersapi.ItemCancelled ||
			it.KOTStatus == ordersapi.ItemKOTCancelled
		if cancelling {
			return apperr.New("ITEM_IN_PREPARATION", apperr.CategoryBusinessRule,
				"\""+cur.ProductName+"\" is already "+cur.KOTStatus+" in the kitchen and cannot be cancelled.").
				WithFields(apperr.FieldError{Field: fmt.Sprintf("items[%d]", i), Code: "IN_PREPARATION", Message: "Already " + cur.KOTStatus + "."}).
				WithMeta(map[string]any{"orderItemId": cur.ID, "kotStatus": cur.KOTStatus})
		}
		if it.Quantity > 0 && it.Quantity != cur.Quantity {
			return apperr.New("ITEM_IN_PREPARATION", apperr.CategoryBusinessRule,
				"\""+cur.ProductName+"\" is already "+cur.KOTStatus+" in the kitchen; its quantity cannot change. Add a new line instead.").
				WithFields(apperr.FieldError{Field: fmt.Sprintf("items[%d].quantity", i), Code: "IN_PREPARATION", Message: "Already " + cur.KOTStatus + "."}).
				WithMeta(map[string]any{"orderItemId": cur.ID, "kotStatus": cur.KOTStatus})
		}
	}
	return nil
}

/*
 * Updating an order.
 *
 * The one endpoint for everything that happens to an order after it is placed:
 * more items, fewer items, a changed quantity, a line cancelled, a batch sent
 * to the kitchen, a discount applied. The till holds the order while it is
 * being worked on and sends the picture it has; this records it.
 *
 * Lines with an id are updated, lines without are inserted, and lines that go
 * unmentioned are left alone — so a screen can send only what changed without
 * losing the rounds before it. Nothing is deleted, because a line the kitchen
 * has already been handed cannot be made never to have existed; a cancelled
 * one keeps its place and stops counting.
 *
 * The batch number is taken from the line when the till sets one. Only when it
 * does not is the next batch used, which is what "send the rest to the
 * kitchen" means.
 */
func (s *Service) UpdatePOSOrder(ctx context.Context, claims *auth.StaffClaims, orderID string, items []ordersapi.POSItem, charges ordersapi.OrderTotalsPatch, status, notes string, extra *POSOrderExtraFields) (*ordersapi.Order, int, error) {
	return s.UpdatePOSOrderKDS(ctx, claims, orderID, items, charges, status, notes, extra, nil)
}

// UpdatePOSOrderKDS is UpdatePOSOrder plus the KDS keys (see POSKDSFields).
func (s *Service) UpdatePOSOrderKDS(ctx context.Context, claims *auth.StaffClaims, orderID string, items []ordersapi.POSItem, charges ordersapi.OrderTotalsPatch, status, notes string, extra *POSOrderExtraFields, kds *POSKDSFields) (*ordersapi.Order, int, error) {
	if !tenant.HasPermission(claims, tenant.PermOrdersAmend) {
		return nil, 0, apperr.PermissionDenied("You do not have permission to amend orders.")
	}
	order, err := s.GetOrderAdmin(ctx, claims, orderID)
	if err != nil {
		return nil, 0, err
	}
	if order.Status == ordersapi.StatusCancelled || order.Status == ordersapi.StatusReturned {
		return nil, 0, apperr.Validation("This order is closed. Start a new one.")
	}
	if err := assertUpdateHasCustomer(order, extra); err != nil {
		return nil, 0, err
	}

	/*
	 * A split that is still being paid freezes the cart.
	 *
	 * The shares were cut from a total; adding a line, changing a quantity,
	 * cancelling a line or moving a charge would make the shares no longer
	 * add up to the order. So while any share is open, anything that touches
	 * money is refused with one clear message — clear the split (DELETE
	 * /split) or finish paying it, then change the cart. Status, notes and
	 * customer details are not money and still go through.
	 */
	if len(items) > 0 || chargesTouched(charges) {
		pending, err := s.store.HasOpenBills(ctx, orderID)
		if err != nil {
			return nil, 0, apperr.Internal(err)
		}
		if pending {
			return nil, 0, apperr.New("SPLIT_PAYMENT_PENDING", apperr.CategoryBusinessRule,
				"A split payment is pending on this order. Clear the split or complete the payment before updating the cart.")
		}
	}

	if err := validatePOSItems(items); err != nil {
		return nil, 0, err
	}
	for i := range items {
		it := &items[i]
		if it.Status != "" {
			it.Status = ordersapi.NormalizeItemStatus(it.Status)
			if !ordersapi.IsValidItemStatus(it.Status) {
				return nil, 0, apperr.Validation("\"" + it.Status + "\" is not a line status.").
					WithFields(apperr.FieldError{
						Field:   fmt.Sprintf("items[%d].status", i),
						Code:    "INVALID",
						Message: "pending, in_progress, done, cancel, cancelled or returned.",
					})
			}
		}
	}
	status = ordersapi.NormalizeStatus(status)
	if status != "" && !ordersapi.IsValidStatus(status) {
		return nil, 0, apperr.Validation("\"" + status + "\" is not an order status. Valid values: " +
			strings.Join(ordersapi.AllStatuses(), ", ") + ".").
			WithFields(apperr.FieldError{Field: "status", Code: "INVALID", Message: "Not a status."})
	}

	// KDS: validate what the frontend says about the kitchen, and refuse to
	// cancel or change what the kitchen is already making.
	if err := normalizeItemKOT(items, true); err != nil {
		return nil, 0, err
	}
	if err := assertKitchenAllowsEdit(order, items); err != nil {
		return nil, 0, err
	}
	if status == ordersapi.StatusCancelled && ordersapi.KOTInKitchen(order.KOTStatus) {
		return nil, 0, apperr.New("ORDER_IN_PREPARATION", apperr.CategoryBusinessRule,
			"The kitchen is already working on this order ("+order.KOTStatus+"); it cannot be cancelled.").
			WithMeta(map[string]any{"kotStatus": order.KOTStatus})
	}
	/*
	 * Fulfilment and customer, checked before anything is written.
	 *
	 * Moving to delivery picks up the outlet's delivery fee and moving off it
	 * drops the fee, the same as a new order would — unless the till sent a
	 * fee of its own. Either way it is money, so it counts for the split
	 * check below. The customer named by id must exist, and their details are
	 * written first so the purchaser copied onto the order is the updated one.
	 */
	var fulfilment string
	var customerID string
	if extra != nil {
		if extra.FulfilmentType != nil {
			fulfilment = strings.ToLower(strings.TrimSpace(*extra.FulfilmentType))
			switch fulfilment {
			case ordersapi.FulfilmentPickup, ordersapi.FulfilmentDelivery, ordersapi.FulfilmentTakeaway:
			default:
				return nil, 0, apperr.Validation("fulfilmentType must be pickup, delivery or takeaway.").
					WithFields(apperr.FieldError{Field: "fulfilmentType", Code: "INVALID", Message: "pickup, delivery or takeaway."})
			}
			if fulfilment == order.FulfilmentType {
				fulfilment = ""
			}
		}
		if extra.CustomerID != nil {
			customerID = strings.TrimSpace(*extra.CustomerID)
		}
	}
	// New contact details for an order already linked to a customer update
	// that customer too: reads show the linked customer's record, so leaving
	// it alone would show the old name and mobile.
	contactCustomerID := customerID
	if contactCustomerID == "" && order.CustomerID != nil && extra != nil && (extra.Customer != nil || hasGuestContact(extra)) {
		contactCustomerID = strings.TrimSpace(*order.CustomerID)
	}
	origCharges := charges
	if fulfilment != "" && charges.DeliveryFeeCents == nil {
		switch {
		case fulfilment == ordersapi.FulfilmentDelivery && order.DeliveryFeeCents == 0:
			if branch, err := s.branches.GetBranch(ctx, order.BranchID); err == nil {
				if fee := s.posDeliveryFee(ctx, branch, order, order.SubtotalCents); fee > 0 {
					charges.DeliveryFeeCents = &fee
				}
			}
		case order.FulfilmentType == ordersapi.FulfilmentDelivery && order.DeliveryFeeCents > 0:
			var none int64
			charges.DeliveryFeeCents = &none
		}
	}
	if customerID != "" {
		if _, err := s.store.GetCustomerContact(ctx, customerID); err != nil {
			if errors.Is(err, store.ErrCustomerNotFound) {
				return nil, 0, apperr.NotFound("Customer not found.").
					WithFields(apperr.FieldError{Field: "customerId", Code: "NOT_FOUND", Message: "No such customer."})
			}
			return nil, 0, apperr.Internal(err)
		}
	}

	var kotTarget string
	if kds != nil && strings.TrimSpace(kds.KOTStatus) != "" {
		kotTarget = ordersapi.NormalizeKOTStatus(kds.KOTStatus)
		if !ordersapi.IsValidKOTStatus(kotTarget) {
			return nil, 0, apperr.Validation("\"" + kds.KOTStatus + "\" is not a kitchen status. Valid: " +
				strings.Join(ordersapi.AllKOTStatuses(), ", ") + ".").
				WithFields(apperr.FieldError{Field: "kotStatus", Code: "INVALID", Message: "Not a KOT status."})
		}
	}

	// A fulfilment change that moved the delivery fee touches money too.
	if len(items) == 0 && !chargesTouched(origCharges) && chargesTouched(charges) {
		pending, err := s.store.HasOpenBills(ctx, orderID)
		if err != nil {
			return nil, 0, apperr.Internal(err)
		}
		if pending {
			return nil, 0, apperr.New("SPLIT_PAYMENT_PENDING", apperr.CategoryBusinessRule,
				"A split payment is pending on this order. Clear the split or complete the payment before updating the cart.")
		}
	}

	items, err = s.pricePOSItemsFromCatalog(ctx, order.BranchID, items)
	if err != nil {
		return nil, 0, err
	}

	// The customer record first, so the purchaser copied onto the order
	// below is the updated one.
	if contactCustomerID != "" && (extra.Customer != nil || hasGuestContact(extra)) {
		first, last, email, phone := customerContactFrom(extra)
		if err := s.store.UpdateCustomerContact(ctx, contactCustomerID, first, last, email, phone); err != nil {
			if errors.Is(err, store.ErrCustomerContactTaken) {
				return nil, 0, apperr.New("CUSTOMER_CONTACT_TAKEN", apperr.CategoryConflict,
					"That email or mobile number already belongs to another customer.").
					WithFields(apperr.FieldError{Field: "customer", Code: "TAKEN", Message: "Email or mobile in use."})
			}
			if errors.Is(err, store.ErrCustomerNotFound) {
				return nil, 0, apperr.NotFound("Customer not found.")
			}
			return nil, 0, apperr.Internal(err)
		}
	}

	batchNo, err := s.store.NextBatchNo(ctx, orderID)
	if err != nil {
		return nil, 0, apperr.Internal(err)
	}
	if err := s.store.UpsertPOSItems(ctx, orderID, batchNo, items); err != nil {
		return nil, 0, apperr.Internal(err)
	}
	if notes != "" {
		if err := s.store.SetOrderNotes(ctx, orderID, notes); err != nil {
			return nil, 0, apperr.Internal(err)
		}
	}
	if fulfilment != "" {
		if err := s.store.SetOrderFulfilment(ctx, orderID, fulfilment); err != nil {
			return nil, 0, apperr.Internal(err)
		}
	}
	// Over the order's own charges, so anything the till did not mention
	// survives the batch rather than being zeroed.
	merged := charges.Apply(ordersapi.OrderTotals{
		DiscountPct:        order.DiscountPct,
		DiscountCents:      order.DiscountCents,
		TaxCents:           order.TaxCents,
		TaxPct:             s.orderTaxPct(ctx, order),
		DeliveryFeeCents:   order.DeliveryFeeCents,
		ServiceChargeCents: order.ServiceChargeCents,
		TipCents:           order.TipCents,
	})
	if err := s.recalculatePOSTotals(ctx, orderID, merged); err != nil {
		return nil, 0, err
	}

	if status != "" && status != order.Status {
		if _, err := s.store.UpdateOrderStatus(ctx, orderID, status, "staff", staffIDPtr(claims), notes); err != nil {
			return nil, 0, apperr.Internal(err)
		}
	}

	if extra != nil {
		fillGuestFromCustomer(extra)
		// The order's purchaser copy follows the customer whose details were
		// just written (receipts, notices and payment links go to it); an
		// order with no customer keeps the typed details itself.
		orderStreet, orderState := extra.Customer.streetAndState()
		// A new address line without a street (an older till) is the street
		// too, as on the customer record — not the old street beside it.
		if orderStreet == nil && extra.GuestCustomerAddress != nil && strings.TrimSpace(*extra.GuestCustomerAddress) != "" {
			orderStreet = extra.GuestCustomerAddress
		}
		linkID := extra.CustomerID
		if linkID == nil && contactCustomerID != "" && !extra.ClearCustomerID {
			linkID = &contactCustomerID
		}
		if contactCustomerID == "" && !extra.ClearCustomerID && (extra.Customer != nil || hasGuestContact(extra)) {
			first, last, email, phone := customerContactFrom(extra)
			if err := s.store.SetOrderPurchaserContact(ctx, orderID, first, last, email, phone); err != nil {
				return nil, 0, apperr.Internal(err)
			}
		}
		if err := s.store.UpdateOrderExtraFields(ctx, orderID,
			linkID,
			extra.ClearCustomerID,
			extra.GuestCustomerName,
			extra.GuestCustomerMobile,
			extra.GuestCustomerAddress,
			extra.GuestCustomerEmail,
			extra.OrderDeliverDate,
			extra.OrderDeliverTime,
			extra.GuestCustomerCity,
			extra.GuestCustomerPostcode,
			orderStreet,
			orderState,
		); err != nil {
			return nil, 0, apperr.Internal(err)
		}
		// The customer record too, not just the order: the address lives on
		// customer_addresses, which is where the order GET reads it back
		// from. For the customer the request names, or the order's own
		// customer when the request sends their details.
		street, state := extra.Customer.streetAndState()
		if extra.Customer != nil {
			// The customer block is taken as sent: a field it leaves out or
			// sends null is cleared, on the order and on the customer record,
			// so the GET returns only what was given.
			c, t := extra.Customer, strings.TrimSpace
			if err := s.store.SetOrderCustomerAddress(ctx, orderID, t(c.Address), t(c.Street), t(c.City), t(c.State), t(c.Postcode)); err != nil {
				return nil, 0, apperr.Internal(err)
			}
			if contactCustomerID != "" {
				if err := s.saveCustomerAddressAsSent(ctx, contactCustomerID, c); err != nil {
					return nil, 0, apperr.Internal(err)
				}
			}
		} else if contactCustomerID != "" && (extra.GuestCustomerAddress != nil || extra.GuestCustomerCity != nil ||
			extra.GuestCustomerPostcode != nil || street != nil || state != nil) {
			if err := s.store.UpsertCustomerAddress(ctx, contactCustomerID, extra.GuestCustomerAddress, street,
				extra.GuestCustomerCity, state, extra.GuestCustomerPostcode); err != nil {
				return nil, 0, apperr.Internal(err)
			}
		}
		if extra.Recipient != nil {
			if err := s.store.UpdateOrderRecipient(ctx, orderID, *extra.Recipient); err != nil {
				return nil, 0, apperr.Internal(err)
			}
		}
		if extra.GiftMessage != nil || extra.GiftReason != nil || extra.IsGift != nil {
			if err := s.store.UpdateOrderGift(ctx, orderID, extra.GiftMessage, extra.GiftReason, extra.IsGift); err != nil {
				return nil, 0, apperr.Internal(err)
			}
		}
	}

	// Every update from the till stamps who made it.
	if err := s.store.SetUpdatedBiller(ctx, orderID, staffIDPtr(claims)); err != nil {
		return nil, 0, apperr.Internal(err)
	}

	// KDS keys, after the update itself has gone through.
	event := realtime.EventOrderUpdated
	if kds != nil {
		if kotTarget != "" {
			if err := s.store.SetOrderKOTStatusRaw(ctx, orderID, kotTarget, store.KOTActor{
				ID: staffIDPtr(claims), Type: "staff", Source: "pos", Note: "Set from POS"}); err != nil {
				return nil, 0, apperr.Internal(err)
			}
		}
		if kds.MarkAmended {
			at := kds.AmendedAt
			if at == nil {
				at = timex.EventTimeFrom(ctx)
			}
			if err := s.store.MarkAmended(ctx, orderID, at); err != nil {
				return nil, 0, apperr.Internal(err)
			}
			event = realtime.EventOrderAmended
		}
	}

	s.writeOrderAudit(ctx, claims, "pos.order.update", orderID,
		map[string]any{"batchNo": batchNo, "lines": len(items), "status": status, "fulfilmentType": fulfilment, "customerId": customerID})

	updated, err := s.store.GetOrderByID(ctx, orderID)
	if err != nil {
		return nil, 0, apperr.Internal(err)
	}
	s.publishOrder(event, updated, "")
	return updated, batchNo, nil
}

/*
 * Deleting an order.
 *
 * A till entry that should never have existed — rung up against the wrong
 * table, or twice. It disappears from every list rather than sitting there as
 * a cancellation somebody has to explain. The row survives, because "it was
 * never here" is a claim the books should still be able to check.
 */
func (s *Service) DeletePOSOrder(ctx context.Context, claims *auth.StaffClaims, orderID string) error {
	if !tenant.HasPermission(claims, tenant.PermOrdersCancel) {
		return apperr.PermissionDenied("You do not have permission to remove orders.")
	}
	existing, err := s.GetOrderAdmin(ctx, claims, orderID)
	if err != nil {
		return err
	}
	if err := s.store.SoftDeleteOrder(ctx, orderID); err != nil {
		return notFoundOrInternal(err, "Order not found.")
	}
	s.writeOrderAudit(ctx, claims, "pos.order.delete", orderID, nil)
	// Gone from every list; screens showing it refetch and drop it.
	s.publishOrder(realtime.EventOrderUpdated, existing, "")
	return nil
}
