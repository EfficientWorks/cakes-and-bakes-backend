package domain

import (
	"bakehouse/pkg/realtime"
	"bakehouse/pkg/timex"
	"bakehouse/services/orders/internal/store"
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/audit"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/tenant"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/orders/ordersapi"
)

/*
 * Point of sale.
 *
 * The counter is a different transaction to the website, and the difference is
 * not the data but the certainty. Online, the customer might be looking at a
 * stale price, might have picked a slot that has since filled, might be
 * ordering something that sold out while they typed their address — so the
 * checkout argues with them. At a till the operator has the goods, the
 * customer and the price all in one place, and an argument is just an obstacle.
 *
 * So this path inserts what it is told and does the arithmetic. The one thing
 * it will not delegate is the money: totals are computed here from the lines,
 * because a till that could name its own total is a till that can be wrong
 * about what it took.
 */

// POSOrderRequest is a ticket being sent from the counter.
type POSOrderRequest struct {
	BranchID       string
	FulfilmentType string

	ScheduledDate *time.Time
	SlotStart     *string
	SlotEnd       *string

	// Optional. A walk-in has nobody attached; a regular being served at the
	// counter should still land on their record.
	CustomerID *string
	Customer   *POSCustomer

	GuestCustomerName     *string
	GuestCustomerMobile   *string
	GuestCustomerAddress  *string
	GuestCustomerCity     *string
	GuestCustomerPostcode *string
	GuestCustomerEmail    *string
	OrderDeliverDate      *string
	OrderDeliverTime      *string

	Items []ordersapi.POSItem

	// Order-level charges, as the counter applied them.
	DiscountPct        *float64
	DiscountCents      int64
	TaxPct             *float64
	TaxCents           int64
	DeliveryFeeCents   int64
	ServiceChargeCents int64
	TipCents           int64

	Notes  string
	Status string

	// Who the order is for, when not the customer (storefront-style
	// recipient). Nil: the recipient is the customer, as before.
	Recipient *ordersapi.POSRecipient
	// Gift message and reason (occasion), and whether it is a gift.
	GiftMessage *string
	GiftReason  *string
	IsGift      *bool

	// KDS: the order's kitchen status as the till states it at creation.
	// Empty: the order joins the queue (the column default).
	KOTStatus string
	// KDS: flag the new order "Amended" on the kitchen display.
	MarkAmended bool
}

// POSCustomer is somebody attached to a walk-in order.
type POSCustomer struct {
	FirstName string
	LastName  string
	Phone     string
	Email     string
	Address   string
	Street    string
	City      string
	State     string
	Postcode  string
}

// streetAndState is the customer block's street and state, nil when blank.
func (c *POSCustomer) streetAndState() (street, state *string) {
	if c == nil {
		return nil, nil
	}
	return strPtr(c.Street), strPtr(c.State)
}

// POSOrderExtraFields carries optional guest-customer and delivery metadata
// that can be set or updated independently of items and charges.
type POSOrderExtraFields struct {
	CustomerID            *string
	ClearCustomerID       bool // request sent an explicit null: detach the customer
	GuestCustomerName     *string
	GuestCustomerMobile   *string
	GuestCustomerAddress  *string
	GuestCustomerCity     *string
	GuestCustomerPostcode *string
	GuestCustomerEmail    *string
	OrderDeliverDate      *string
	OrderDeliverTime      *string

	// Customer details to write onto the customer record named by
	// CustomerID. Ignored when no customer id is sent: an update without one
	// never changes a customer record.
	Customer *POSCustomer
	// Who the order is for. Nil leaves the recipient as it is.
	Recipient *ordersapi.POSRecipient
	// pickup, delivery or takeaway. Nil leaves it as it is.
	FulfilmentType *string
	// Gift details. Nil keeps; "" clears the message or reason.
	GiftMessage *string
	GiftReason  *string
	IsGift      *bool
}

// POSKDSFields are the KDS keys a POS/admin update may carry. All optional.
type POSKDSFields struct {
	// MarkAmended flags the order "Amended" on the KDS (the frontend decides
	// when a change matters to the kitchen).
	MarkAmended bool
	AmendedAt   *time.Time
	// KOTStatus sets the order's kitchen status as the frontend states it.
	KOTStatus string
}

const posMaxLines = 200

/*
 * Sending the ticket.
 *
 * "Send to kitchen" is what places the order — there is no separate cart to
 * keep, because the cart is the screen the operator is looking at and it lives
 * for as long as they are looking at it. Persisting it would mean two things
 * that could disagree about what the customer asked for.
 */
func (s *Service) CreatePOSOrder(ctx context.Context, claims *auth.StaffClaims, req POSOrderRequest) (*ordersapi.Order, error) {
	if !tenant.HasPermission(claims, tenant.PermOrdersCreateManual) {
		return nil, apperr.PermissionDenied("You do not have permission to take orders.")
	}
	if strings.TrimSpace(req.BranchID) == "" {
		return nil, apperr.Validation("An outlet is required.").
			WithFields(apperr.FieldError{Field: "branchId", Code: "REQUIRED", Message: "Required."})
	}
	branch, err := s.branches.GetBranch(ctx, req.BranchID)
	if err != nil {
		return nil, apperr.NotFound("Outlet not found.")
	}
	if !tenant.CanAccessBranch(claims, "", branch.ID) {
		return nil, apperr.PermissionDenied("You do not have access to this outlet.")
	}
	// A suspended, closed or emergency-closed store takes no new orders at
	// the till either, not only on the website.
	if err := assertBranchOpen(branch); err != nil {
		return nil, err
	}
	if len(req.Items) == 0 {
		return nil, apperr.Validation("Add something to the order first.")
	}
	if len(req.Items) > posMaxLines {
		return nil, apperr.Validation(fmt.Sprintf("An order cannot hold more than %d lines.", posMaxLines))
	}
	if err := validatePOSItems(req.Items); err != nil {
		return nil, err
	}
	if err := assertCreateHasCustomer(req); err != nil {
		return nil, err
	}
	if err := normalizeItemKOT(req.Items, false); err != nil {
		return nil, err
	}
	var kotTarget string
	if strings.TrimSpace(req.KOTStatus) != "" {
		kotTarget = ordersapi.NormalizeKOTStatus(req.KOTStatus)
		if !ordersapi.IsValidKOTStatus(kotTarget) {
			return nil, apperr.Validation("\"" + req.KOTStatus + "\" is not a kitchen status. Valid: " +
				strings.Join(ordersapi.AllKOTStatuses(), ", ") + ".").
				WithFields(apperr.FieldError{Field: "kotStatus", Code: "INVALID", Message: "Not a KOT status."})
		}
	}

	status := ordersapi.NormalizeStatus(req.Status)
	if status == "" {
		status = ordersapi.StatusConfirmed
	}
	if !ordersapi.IsValidStatus(status) {
		return nil, apperr.Validation("\"" + req.Status + "\" is not an order status. Valid values: " +
			strings.Join(ordersapi.AllStatuses(), ", ") + ".").
			WithFields(apperr.FieldError{Field: "status", Code: "INVALID", Message: "Not a status."})
	}
	req.Status = status

	items, err := s.pricePOSItemsFromCatalog(ctx, branch.ID, req.Items)
	if err != nil {
		return nil, err
	}

	// The biller is whoever holds the token, never something the request
	// gets to say.
	shell := posOrderShell(req)
	shell.BillerID = staffIDPtr(claims)
	shell.Status = req.Status

	/*
	 * The customer as the till sent them, for this order and for their record.
	 *
	 * The purchaser used to be filled from the stored customer record whenever
	 * the till sent the name and mobile only as guest fields, and the record
	 * itself was never updated on create. So a customer picked by id, or
	 * matched on their mobile, came back with their old name and number, and
	 * every later read (which shows the linked customer's record) did too.
	 */
	contact := &POSOrderExtraFields{Customer: req.Customer, GuestCustomerName: req.GuestCustomerName,
		GuestCustomerMobile: req.GuestCustomerMobile, GuestCustomerEmail: req.GuestCustomerEmail}
	first, last, email, phone := customerContactFrom(contact)
	if first != nil {
		shell.FirstName = *first
		shell.LastName = derefStr(last)
	}
	if email != nil {
		shell.Email = *email
	}
	if phone != nil {
		shell.Phone = *phone
	}
	if req.CustomerID != nil && strings.TrimSpace(*req.CustomerID) != "" && (first != nil || email != nil || phone != nil) {
		if err := s.store.UpdateCustomerContact(ctx, strings.TrimSpace(*req.CustomerID), first, last, email, phone); err != nil {
			if errors.Is(err, store.ErrCustomerContactTaken) {
				return nil, apperr.New("CUSTOMER_CONTACT_TAKEN", apperr.CategoryConflict,
					"That email or mobile number already belongs to another customer.").
					WithFields(apperr.FieldError{Field: "customer", Code: "TAKEN", Message: "Email or mobile in use."})
			}
			if errors.Is(err, store.ErrCustomerNotFound) {
				return nil, apperr.NotFound("Customer not found.").
					WithFields(apperr.FieldError{Field: "customerId", Code: "NOT_FOUND", Message: "No such customer."})
			}
			return nil, apperr.Internal(err)
		}
	}

	order, err := s.store.CreatePOSOrder(ctx, branch.ID, req.CustomerID, shell, items)
	if err == nil && req.Customer != nil && order.CustomerID != nil && strings.TrimSpace(*order.CustomerID) != "" {
		// As on update: the customer's address is saved exactly as sent.
		if aerr := s.saveCustomerAddressAsSent(ctx, *order.CustomerID, req.Customer); aerr != nil {
			return nil, apperr.Internal(aerr)
		}
	}
	if err != nil {
		if errors.Is(err, store.ErrCustomerContactTaken) {
			return nil, errContactTaken()
		}
		var exists *store.CustomerExistsError
		if errors.As(err, &exists) {
			return nil, errCustomerExists(exists)
		}
		return nil, apperr.Internal(err)
	}

	charges := posCharges(req)
	// The rate: what the till sent, else the outlet's. Kept on the order so
	// every later recalculation (a line discount, a returned item) moves the
	// tax with the subtotal instead of leaving stale cents behind.
	taxPct := req.TaxPct
	if taxPct == nil {
		taxPct = branch.TaxRatePct
	}
	charges.TaxPct = taxPct
	charges.TaxExplicit = charges.TaxCents > 0
	// No fee from the till: the store's delivery pricing for this address,
	// order size and slot. A fee the till sends is staff's call and is kept.
	if req.FulfilmentType == ordersapi.FulfilmentDelivery && charges.DeliveryFeeCents == 0 {
		var subtotal int64
		for _, it := range items {
			subtotal += it.LineTotalCents
		}
		// The address it goes to: the recipient's when the till named one (that
		// becomes the order's delivery address), else the customer's.
		where := &ordersapi.Order{
			GuestCustomerAddress: req.GuestCustomerAddress, GuestCustomerCity: req.GuestCustomerCity,
			GuestCustomerPostcode: req.GuestCustomerPostcode, SlotStart: derefStr(req.SlotStart),
		}
		if r := req.Recipient; r != nil {
			where.DeliveryStreet, where.DeliveryCity, where.DeliveryPostcode = r.Address, r.City, r.Postcode
		}
		charges.DeliveryFeeCents = s.posDeliveryFee(ctx, branch, where, subtotal)
	}

	if err := s.recalculatePOSTotals(ctx, order.ID, charges); err != nil {
		return nil, err
	}

	// KDS keys, once the order exists. A new order is already in the queue,
	// so only a different status is written (and logged on the KOT trail).
	if kotTarget != "" && kotTarget != ordersapi.KOTQueue {
		if err := s.store.SetOrderKOTStatusRaw(ctx, order.ID, kotTarget, store.KOTActor{
			ID: staffIDPtr(claims), Type: "staff", Source: "pos", Note: "Set from POS"}); err != nil {
			return nil, apperr.Internal(err)
		}
	}
	if req.MarkAmended {
		if err := s.store.MarkAmended(ctx, order.ID, timex.EventTimeFrom(ctx)); err != nil {
			return nil, apperr.Internal(err)
		}
	}

	s.writeOrderAudit(ctx, claims, "pos.order.create", order.ID,
		map[string]any{"lines": len(items), "branchId": branch.ID})

	created, err := s.store.GetOrderByID(ctx, order.ID)
	if err == nil {
		s.publishOrder(realtime.EventOrderCreated, created, "")
	}
	return created, err
}

/*
 * normalizeItemKOT validates the kotStatus the frontend sent on each line.
 * New lines default to "queue" in the database; an existing line without one
 * keeps what it has.
 */
func normalizeItemKOT(items []ordersapi.POSItem, _ bool) error {
	for i := range items {
		if strings.TrimSpace(items[i].KOTStatus) == "" {
			continue
		}
		v := ordersapi.NormalizeKOTStatus(items[i].KOTStatus)
		if !ordersapi.IsValidItemKOTStatus(v) {
			return apperr.Validation("\"" + items[i].KOTStatus + "\" is not an item kitchen status.").
				WithFields(apperr.FieldError{Field: fmt.Sprintf("items[%d].kotStatus", i), Code: "INVALID",
					Message: "queue, preparing, ready, completed or cancelled."})
		}
		items[i].KOTStatus = v
	}
	return nil
}

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

/*
 * Handing goods back.
 *
 * Either the whole order or particular lines. The quantity comes off the line
 * and the order's totals are recomputed, so every report that reads the order
 * sees what was actually kept. Money is handed back at the counter and
 * reconciled against the return record — there is no card to credit yet.
 */
func (s *Service) ReturnPOSOrder(ctx context.Context, claims *auth.StaffClaims, orderID string, req ordersapi.ReturnRequest) (string, ordersapi.ReturnCharges, *ReturnRefundResult, error) {
	var none ordersapi.ReturnCharges
	if !tenant.HasPermission(claims, tenant.PermOrdersRefundLimited) {
		return "", none, nil, apperr.PermissionDenied("You do not have permission to process returns.")
	}
	order, err := s.GetOrderAdmin(ctx, claims, orderID)
	if err != nil {
		return "", none, nil, err
	}
	if !req.FullReturn && len(req.Items) == 0 {
		return "", none, nil, apperr.Validation("Choose what is being returned.")
	}
	// A website order can come back only once it has been handed over.
	if order.Channel == "web" && order.Status != ordersapi.StatusFulfilled && !ordersapi.KOTHandedOff(order.KOTStatus) {
		return "", none, nil, apperr.New("ORDER_NOT_FULFILLED", apperr.CategoryBusinessRule,
			"This order has not been fulfilled yet, so nothing can be returned.")
	}
	for _, line := range req.Items {
		if line.Quantity <= 0 {
			return "", none, nil, apperr.Validation("A returned quantity must be at least one.")
		}
	}
	if err := validateReturnCharges(req); err != nil {
		return "", none, nil, err
	}
	plan, err := normalizeReturnRefund(req.Refund)
	if err != nil {
		return "", none, nil, err
	}

	var staffID *string
	if claims != nil && claims.StaffID != "" {
		staffID = &claims.StaffID
	}

	// The rate to refund tax at: the order's or the store's, else the rate
	// the order's own tax amount works out to, so tax is refunded either way.
	taxRate := s.orderTaxPct(ctx, order)
	if taxRate == nil {
		if base := order.SubtotalCents - order.DiscountCents; order.TaxCents > 0 && base > 0 {
			r := float64(order.TaxCents) * 100 / float64(base)
			taxRate = &r
		}
	}
	var orderDiscountShare int64
	pricer := returnPricer(req, order, taxRate, &orderDiscountShare)
	// With a card refund through Stripe, the refund is made before
	// the return commits: a refund the provider refuses leaves no return.
	var refund *ReturnRefundResult
	var refundFirst func(ordersapi.ReturnCharges) error
	if plan != nil {
		refund = &ReturnRefundResult{PaymentMode: plan.PaymentMode, CardType: plan.CardType, RefundType: plan.RefundType}
		refundFirst = func(charges ordersapi.ReturnCharges) error {
			return s.refundReturn(ctx, order, plan, charges, req.Reason, derefStr(staffID), refund)
		}
	}
	returnID, charges, err := s.store.RecordReturn(ctx, orderID, req, staffID, pricer, refundFirst)
	if err != nil {
		var ae *apperr.Error
		if errors.As(err, &ae) {
			return "", none, nil, err
		}
		return "", none, nil, apperr.Validation("That return could not be recorded — check the quantities against what is still outstanding.")
	}
	if refund != nil && refund.Outcome != nil {
		_ = s.store.UpdatePaymentStatus(ctx, orderID, paymentStatusAfterRefund(refund.Outcome))
	}

	// The order is now worth less than it was. Recomputed from the lines so
	// every reader agrees, rather than subtracting from a stored total.
	if err := s.recalculateAfterReturn(ctx, orderID, orderDiscountShare, taxRate); err != nil {
		return "", none, nil, err
	}
	_ = s.store.SetUpdatedBiller(ctx, orderID, staffID)

	audit := map[string]any{"returnId": returnID, "amountCents": charges.GrossCents, "full": req.FullReturn,
		"discountCents": charges.DiscountCents, "taxCents": charges.TaxCents, "refundCents": charges.RefundCents}
	if refund != nil {
		audit["refund"] = map[string]any{"paymentMode": refund.PaymentMode, "cardType": refund.CardType,
			"refundType": refund.RefundType, "amountCents": refund.AmountCents, "status": refund.Status}
	}
	s.writeOrderAudit(ctx, claims, "pos.order.return", orderID, audit)
	s.publishOrderByID(ctx, realtime.EventOrderUpdated, orderID)
	// A refund the provider settled at once is final now; a pending one is
	// announced when its webhook arrives.
	if refund != nil && refund.Outcome != nil && refund.Status == RefundSucceeded {
		s.publishPaymentEvent(ctx, EventRefundCompleted, orderID, map[string]any{
			"provider": refund.CardType, "amountCents": refund.AmountCents, "returnId": returnID,
		})
	}

	return returnID, charges, refund, nil
}

// validateReturnCharges checks the discount and tax the till sent on a return.
func validateReturnCharges(req ordersapi.ReturnRequest) error {
	if req.DiscountPct != nil && (*req.DiscountPct < 0 || *req.DiscountPct > 100) {
		return apperr.Validation("discountPct must be between 0 and 100.").
			WithFields(apperr.FieldError{Field: "discountPct", Code: "INVALID", Message: "0 to 100."})
	}
	if req.DiscountCents != nil && *req.DiscountCents < 0 {
		return apperr.Validation("discountCents cannot be negative.").
			WithFields(apperr.FieldError{Field: "discountCents", Code: "INVALID", Message: "Cannot be negative."})
	}
	if req.TaxPct != nil && (*req.TaxPct < 0 || *req.TaxPct > 100) {
		return apperr.Validation("taxPct must be between 0 and 100.").
			WithFields(apperr.FieldError{Field: "taxPct", Code: "INVALID", Message: "0 to 100."})
	}
	if req.TaxCents != nil && *req.TaxCents < 0 {
		return apperr.Validation("taxCents cannot be negative.").
			WithFields(apperr.FieldError{Field: "taxCents", Code: "INVALID", Message: "Cannot be negative."})
	}
	return nil
}

/*
 * returnPricer prices a return line by line.
 *
 * Each returned line on its own:
 *
 *	gross    = unit price × quantity returned
 *	discount = the line's own discount, prorated by the quantity returned
 *	           (a line of 4 with $2 off returns $0.50 per unit), plus the
 *	           line's share of the order discount (the order's rate, or a
 *	           fixed order discount shared by value) on what is left
 *	tax      = the tax rate on gross − discount
 *	refund   = gross − discount + tax
 *
 * What the till states for the return wins: a discount or tax percentage is
 * applied to every line; a cents amount is shared across the lines by value
 * (largest remainder, so the shares add up exactly). The return's totals are
 * the sums of its lines, so item, return and order figures always agree.
 */
//
// orderShareOut receives the part of a fixed order discount that went back
// with these goods, so the order keeps only the rest (recalculateAfterReturn).
func returnPricer(req ordersapi.ReturnRequest, order *ordersapi.Order, orderTaxPct *float64, orderShareOut *int64) ordersapi.ReturnPricer {
	return func(lines []ordersapi.ReturnLineInput) ([]ordersapi.ReturnLineCharges, ordersapi.ReturnCharges) {
		n := len(lines)
		gross := make([]int64, n)
		for i, l := range lines {
			gross[i] = l.UnitPriceCents * int64(l.Quantity)
		}

		// The fixed order discount's share for these goods, from what the
		// order is worth now (both the subtotal and the discount shrink with
		// each return, so the ratio holds across repeated returns).
		orderShare := make([]int64, n)
		if (order.DiscountPct == nil || *order.DiscountPct <= 0) && order.DiscountCents > 0 && order.SubtotalCents > 0 {
			nets := make([]int64, n)
			for i, l := range lines {
				nets[i] = clampNonNegative(gross[i] - min(proratedLineDiscount(l), gross[i]))
			}
			var netSum int64
			for _, v := range nets {
				netSum += v
			}
			share := int64(math.Round(float64(order.DiscountCents) * float64(netSum) / float64(order.SubtotalCents)))
			if share > order.DiscountCents {
				share = order.DiscountCents
			}
			orderShare = shareByWeight(share, nets)
			if orderShareOut != nil {
				*orderShareOut = share
			}
		}

		// Discount per line.
		disc := make([]int64, n)
		switch {
		case req.DiscountCents != nil:
			disc = shareByWeight(*req.DiscountCents, gross)
		case req.DiscountPct != nil:
			for i := range lines {
				disc[i] = int64(math.Round(float64(gross[i]) * *req.DiscountPct / 100))
			}
		default:
			for i, l := range lines {
				own := proratedLineDiscount(l)
				if own > gross[i] {
					own = gross[i]
				}
				net := gross[i] - own
				share := orderShare[i]
				if order.DiscountPct != nil && *order.DiscountPct > 0 {
					share = int64(math.Round(float64(net) * *order.DiscountPct / 100))
				}
				disc[i] = own + share
			}
		}
		for i := range disc {
			if disc[i] > gross[i] {
				disc[i] = gross[i]
			}
			if disc[i] < 0 {
				disc[i] = 0
			}
		}

		// Tax per line, on what is left after its discount.
		taxable := make([]int64, n)
		for i := range lines {
			taxable[i] = gross[i] - disc[i]
		}
		rate := req.TaxPct
		if rate == nil {
			rate = orderTaxPct
		}
		tax := make([]int64, n)
		switch {
		case req.TaxCents != nil:
			tax = shareByWeight(*req.TaxCents, taxable)
		case rate != nil && *rate > 0:
			for i := range lines {
				tax[i] = taxOn(taxable[i], rate)
			}
		}

		out := make([]ordersapi.ReturnLineCharges, n)
		var total ordersapi.ReturnCharges
		for i, l := range lines {
			c := ordersapi.ReturnCharges{GrossCents: gross[i], DiscountCents: disc[i], TaxCents: tax[i]}
			c.DiscountPct = req.DiscountPct
			if c.DiscountPct == nil {
				c.DiscountPct = pctOf(disc[i], gross[i], 100)
			}
			c.TaxPct = taxRateOf(req, rate, tax[i], taxable[i])
			c.RefundCents = clampNonNegative(gross[i] - disc[i] + tax[i])
			out[i] = ordersapi.ReturnLineCharges{OrderItemID: l.OrderItemID, Quantity: l.Quantity, ReturnCharges: c}

			total.GrossCents += c.GrossCents
			total.DiscountCents += c.DiscountCents
			total.TaxCents += c.TaxCents
			total.RefundCents += c.RefundCents
		}
		total.DiscountPct = req.DiscountPct
		if total.DiscountPct == nil {
			total.DiscountPct = pctOf(total.DiscountCents, total.GrossCents, 100)
		}
		total.TaxPct = taxRateOf(req, rate, total.TaxCents, total.GrossCents-total.DiscountCents)
		return out, total
	}
}

// proratedLineDiscount is the part of a line's own discount that belongs to
// the units coming back.
func proratedLineDiscount(l ordersapi.ReturnLineInput) int64 {
	if l.LineDiscountCents > 0 && l.LineQuantity > 0 {
		return int64(math.Round(float64(l.LineDiscountCents) * float64(l.Quantity) / float64(l.LineQuantity)))
	}
	if l.LineDiscountPct != nil && *l.LineDiscountPct > 0 {
		return int64(math.Round(float64(l.UnitPriceCents*int64(l.Quantity)) * *l.LineDiscountPct / 100))
	}
	return 0
}

// taxRateOf is the rate to report: the one applied, or, when the till gave a
// tax amount, what that amount comes to on the taxable value.
func taxRateOf(req ordersapi.ReturnRequest, rate *float64, tax, taxable int64) *float64 {
	if req.TaxCents != nil {
		if req.TaxPct != nil {
			return req.TaxPct
		}
		return pctOf(tax, taxable, 1000)
	}
	if rate != nil && *rate > 0 {
		return rate
	}
	return nil
}

// pctOf is part/whole as a percentage rounded to 1/scale of a percent, or nil
// when there is nothing to report.
func pctOf(part, whole int64, scale float64) *float64 {
	if part <= 0 || whole <= 0 {
		return nil
	}
	p := math.Round(float64(part)*100*scale/float64(whole)) / scale
	return &p
}

func clampNonNegative(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}

// shareByWeight splits total across the weights in proportion, by largest
// remainder, so the shares add up to exactly total.
func shareByWeight(total int64, weights []int64) []int64 {
	out := make([]int64, len(weights))
	var sum int64
	for _, w := range weights {
		if w > 0 {
			sum += w
		}
	}
	if total <= 0 || sum == 0 {
		return out
	}
	type rem struct {
		i int
		r float64
	}
	var assigned int64
	rems := make([]rem, 0, len(weights))
	for i, w := range weights {
		if w <= 0 {
			continue
		}
		exact := float64(total) * float64(w) / float64(sum)
		out[i] = int64(math.Floor(exact))
		assigned += out[i]
		rems = append(rems, rem{i, exact - math.Floor(exact)})
	}
	sort.Slice(rems, func(a, b int) bool { return rems[a].r > rems[b].r })
	for k := 0; assigned < total && k < len(rems); k++ {
		out[rems[k].i]++
		assigned++
	}
	return out
}

/* ------------------------------------------------------------- arithmetic */

/*
 * The total, computed from the lines.
 *
 * Charges arrive from the till because tax rate, delivery and service are
 * decisions made at the counter; the subtotal never does, because it is a fact
 * about the lines that were stored. A percentage discount is applied to the
 * subtotal here rather than trusted from the request, so what the customer was
 * shown and what the receipt says cannot come apart.
 */
func (s *Service) recalculatePOSTotals(ctx context.Context, orderID string, charges ordersapi.OrderTotals) error {
	lines, err := s.store.LinesForTotals(ctx, orderID)
	if err != nil {
		return apperr.Internal(err)
	}

	var subtotal int64
	for _, l := range lines {
		kept := l.Quantity - l.ReturnedQuantity
		if kept <= 0 {
			continue
		}
		// Per-unit, so a partly returned line contributes only what was kept,
		// and keeps only the kept units' share of its own discount (the
		// returned units' share went back with them, see returnPricer).
		perUnit := l.UnitPriceCents
		subtotal += perUnit * int64(kept)
		if l.DiscountCents == 0 && l.DiscountPct != nil && *l.DiscountPct > 0 {
			// A line discount kept only as a percentage, as the return prices it.
			subtotal -= int64(math.Round(float64(perUnit*int64(kept)) * *l.DiscountPct / 100))
		} else if l.Quantity > 0 && kept < l.Quantity {
			subtotal -= int64(math.Round(float64(l.DiscountCents) * float64(kept) / float64(l.Quantity)))
		} else {
			subtotal -= l.DiscountCents
		}
	}
	if subtotal < 0 {
		subtotal = 0
	}

	discount := charges.DiscountCents
	if charges.DiscountPct != nil && *charges.DiscountPct > 0 {
		pct := *charges.DiscountPct
		if pct > 100 {
			pct = 100
		}
		// Rounded to the nearest cent: a customer offered 10% off £9.99 is
		// owed a penny one way or the other, and truncating always favours
		// the shop.
		discount = int64(math.Round(float64(subtotal) * pct / 100))
	}
	if discount > subtotal {
		discount = subtotal
	}

	// Tax follows the rate unless the till set the cents itself this time.
	if !charges.TaxExplicit && charges.TaxPct != nil && *charges.TaxPct > 0 {
		charges.TaxCents = taxOn(subtotal-discount, charges.TaxPct)
	}

	total := subtotal - discount + charges.TaxCents + charges.DeliveryFeeCents +
		charges.ServiceChargeCents + charges.TipCents
	if total < 0 {
		total = 0
	}

	return s.applyTotals(ctx, orderID, ordersapi.OrderTotals{
		SubtotalCents:      subtotal,
		DiscountCents:      discount,
		DiscountPct:        charges.DiscountPct,
		TaxCents:           charges.TaxCents,
		TaxPct:             charges.TaxPct,
		DeliveryFeeCents:   charges.DeliveryFeeCents,
		ServiceChargeCents: charges.ServiceChargeCents,
		TipCents:           charges.TipCents,
		TotalCents:         total,
	})
}

// recalculateAfterReturn recomputes the order after a return. A fixed order
// discount keeps only what was not refunded with the goods; tax follows the
// rate the return was priced at.
func (s *Service) recalculateAfterReturn(ctx context.Context, orderID string, orderDiscountShare int64, taxPct *float64) error {
	order, err := s.store.GetOrderByID(ctx, orderID)
	if err != nil {
		return apperr.Internal(err)
	}
	discount := order.DiscountCents
	if order.DiscountPct == nil || *order.DiscountPct <= 0 {
		discount = clampNonNegative(discount - orderDiscountShare)
	}
	return s.recalculatePOSTotals(ctx, orderID, ordersapi.OrderTotals{
		DiscountPct:        order.DiscountPct,
		DiscountCents:      discount,
		TaxCents:           order.TaxCents,
		TaxPct:             taxPct,
		DeliveryFeeCents:   order.DeliveryFeeCents,
		ServiceChargeCents: order.ServiceChargeCents,
		TipCents:           order.TipCents,
	})
}

// recalculatePOSTotalsFromStored recomputes using the charges already on the
// order — for a return, where nothing about tax or service has changed.
func (s *Service) recalculatePOSTotalsFromStored(ctx context.Context, orderID string) error {
	order, err := s.store.GetOrderByID(ctx, orderID)
	if err != nil {
		return apperr.Internal(err)
	}
	return s.recalculatePOSTotals(ctx, orderID, ordersapi.OrderTotals{
		DiscountPct:        order.DiscountPct,
		DiscountCents:      order.DiscountCents,
		TaxCents:           order.TaxCents,
		TaxPct:             s.orderTaxPct(ctx, order),
		DeliveryFeeCents:   order.DeliveryFeeCents,
		ServiceChargeCents: order.ServiceChargeCents,
		TipCents:           order.TipCents,
	})
}

func (s *Service) applyTotals(ctx context.Context, orderID string, t ordersapi.OrderTotals) error {
	if err := s.store.ApplyOrderTotals(ctx, orderID, t); err != nil {
		return notFoundOrInternal(err, "Order not found.")
	}
	return nil
}

func validatePOSItems(items []ordersapi.POSItem) error {
	for i, it := range items {
		where := fmt.Sprintf("items[%d]", i)
		/*
		 * A product is required to add a line, not to change one.
		 * For manual / counter items (e.g., custom candle, non-catalog item),
		 * productID can be empty as long as a product name is provided.
		 *
		 * An edit names the line by its id and sends what is changing — a
		 * quantity, a status, the round it is on. Demanding the product again
		 * would make the till echo back fields it is not touching, and the
		 * only thing that could do is let them drift from what is stored.
		 */
		if it.ID == "" && strings.TrimSpace(it.ProductID) == "" && strings.TrimSpace(it.ProductName) == "" {
			return apperr.Validation("Every new line needs a product or item name.").
				WithFields(apperr.FieldError{Field: where + ".productName", Code: "REQUIRED", Message: "Required."})
		}
		if it.ID == "" {
			if it.Quantity <= 0 {
				return apperr.Validation("A quantity must be at least one.").
					WithFields(apperr.FieldError{Field: where + ".quantity", Code: "MIN", Message: "At least 1."})
			}
		} else {
			if it.Quantity < 0 {
				return apperr.Validation("A quantity cannot be negative.").
					WithFields(apperr.FieldError{Field: where + ".quantity", Code: "MIN", Message: "Zero or more."})
			}
		}
		if it.UnitPriceCents < 0 || it.LineTotalCents < 0 || it.DiscountCents < 0 {
			return apperr.Validation("A price cannot be negative.").
				WithFields(apperr.FieldError{Field: where + ".unitPriceCents", Code: "MIN", Message: "Zero or more."})
		}
		if it.DiscountPct != nil && (*it.DiscountPct < 0 || *it.DiscountPct > 100) {
			return apperr.Validation("A discount is between 0 and 100 percent.").
				WithFields(apperr.FieldError{Field: where + ".discountPct", Code: "RANGE", Message: "0 to 100."})
		}
	}
	return nil
}

func posCharges(req POSOrderRequest) ordersapi.OrderTotals {
	return ordersapi.OrderTotals{
		DiscountPct:        req.DiscountPct,
		DiscountCents:      req.DiscountCents,
		TaxCents:           req.TaxCents,
		DeliveryFeeCents:   req.DeliveryFeeCents,
		ServiceChargeCents: req.ServiceChargeCents,
		TipCents:           req.TipCents,
	}
}

func posOrderShell(req POSOrderRequest) ordersapi.POSOrderShell {
	shell := ordersapi.POSOrderShell{
		FulfilmentType:        req.FulfilmentType,
		ScheduledDate:         req.ScheduledDate,
		SlotStart:             req.SlotStart,
		SlotEnd:               req.SlotEnd,
		Notes:                 req.Notes,
		GuestCustomerName:     req.GuestCustomerName,
		GuestCustomerMobile:   req.GuestCustomerMobile,
		GuestCustomerAddress:  req.GuestCustomerAddress,
		GuestCustomerCity:     req.GuestCustomerCity,
		GuestCustomerPostcode: req.GuestCustomerPostcode,
		GuestCustomerEmail:    req.GuestCustomerEmail,
		OrderDeliverDate:      req.OrderDeliverDate,
		OrderDeliverTime:      req.OrderDeliverTime,
	}
	if shell.FulfilmentType == "" {
		shell.FulfilmentType = "pickup"
	}
	shell.Recipient = req.Recipient
	shell.GiftMessage, shell.GiftReason, shell.IsGift = req.GiftMessage, req.GiftReason, req.IsGift
	if req.Customer != nil {
		shell.FirstName = req.Customer.FirstName
		shell.LastName = req.Customer.LastName
		shell.Phone = req.Customer.Phone
		shell.Email = req.Customer.Email
		shell.Address = req.Customer.Address
		if shell.GuestCustomerName == nil && (req.Customer.FirstName != "" || req.Customer.LastName != "") {
			name := strings.TrimSpace(req.Customer.FirstName + " " + req.Customer.LastName)
			if name != "" {
				shell.GuestCustomerName = &name
			}
		}
		if shell.GuestCustomerMobile == nil && req.Customer.Phone != "" {
			phone := req.Customer.Phone
			shell.GuestCustomerMobile = &phone
		}
		if shell.GuestCustomerEmail == nil && req.Customer.Email != "" {
			email := req.Customer.Email
			shell.GuestCustomerEmail = &email
		}
		if shell.GuestCustomerAddress == nil && req.Customer.Address != "" {
			addr := req.Customer.Address
			shell.GuestCustomerAddress = &addr
		}
		if shell.GuestCustomerCity == nil && req.Customer.City != "" {
			city := req.Customer.City
			shell.GuestCustomerCity = &city
		}
		if shell.GuestCustomerPostcode == nil && req.Customer.Postcode != "" {
			pc := req.Customer.Postcode
			shell.GuestCustomerPostcode = &pc
		}
		shell.CustomerStreet, shell.CustomerState = req.Customer.streetAndState()
	}
	return shell
}

// orderTaxPct is the rate an order's tax is computed at: the one stored on
// it, else (for orders from before the rate was kept) the outlet's current
// rate, else nil — in which case the stored cents are left alone.
func (s *Service) orderTaxPct(ctx context.Context, order *ordersapi.Order) *float64 {
	if order.TaxPct != nil {
		return order.TaxPct
	}
	if branch, err := s.branches.GetBranch(ctx, order.BranchID); err == nil && branch != nil {
		return branch.TaxRatePct
	}
	return nil
}

// chargesTouched reports whether a charges patch would change any amount.
func chargesTouched(c ordersapi.OrderTotalsPatch) bool {
	return c.DiscountPct != nil || c.DiscountCents != nil || c.TaxCents != nil || c.TaxPct != nil ||
		c.DeliveryFeeCents != nil || c.ServiceChargeCents != nil || c.TipCents != nil
}

// writeOrderAudit records what the counter did.
//
// Best effort, like every other audit write in this service: losing the
// trail is bad, refusing the sale because the trail could not be written
// is worse.
func (s *Service) writeOrderAudit(ctx context.Context, claims *auth.StaffClaims, action, orderID string, meta map[string]any) {
	if s.audit == nil {
		return
	}
	entry := audit.Entry{
		ActorType:    "staff",
		Action:       action,
		ResourceType: "order",
		ResourceID:   &orderID,
		AfterValue:   meta,
	}
	if claims != nil {
		entry.ActorID = &claims.StaffID
	}
	_ = s.audit.Write(ctx, entry)
}

func staffIDPtr(claims *auth.StaffClaims) *string {
	if claims == nil {
		return nil
	}
	if claims.StaffID != "" {
		return &claims.StaffID
	}
	if claims.Subject != "" {
		return &claims.Subject
	}
	return nil
}

// pricePOSItemsFromCatalog computes base price + modifier price deltas from the
// catalog for new lines (lines without an ID), setting UnitPriceCents and
// LineTotalCents (= UnitPriceCents * Quantity).
func (s *Service) pricePOSItemsFromCatalog(ctx context.Context, branchID string, items []ordersapi.POSItem) ([]ordersapi.POSItem, error) {
	out := make([]ordersapi.POSItem, len(items))
	copy(out, items)

	for i := range out {
		it := &out[i]
		// An edit names the line by its ID and keeps the recorded price;
		// a new line without ID is priced fresh against the catalog.
		if it.ID != "" {
			continue
		}

		// A manual or custom item (like a custom candle or misc counter item) has no catalog product ID.
		// Its pricing and naming are taken directly from the request rather than queried from the catalog.
		if strings.TrimSpace(it.ProductID) == "" {
			if it.LineTotalCents == 0 && it.UnitPriceCents > 0 {
				it.LineTotalCents = it.UnitPriceCents * int64(it.Quantity)
			} else if it.LineTotalCents > 0 && it.UnitPriceCents == 0 && it.Quantity > 0 {
				it.UnitPriceCents = it.LineTotalCents / int64(it.Quantity)
			}
			if it.DiscountPct != nil && *it.DiscountPct > 0 {
				it.DiscountCents = int64(math.Round(float64(it.LineTotalCents) * (*it.DiscountPct) / 100))
			}
			if it.DiscountCents > it.LineTotalCents {
				it.DiscountCents = it.LineTotalCents
			}
			continue
		}

		if s.catalog == nil {
			continue
		}

		var variantID *string
		if strings.TrimSpace(it.VariantID) != "" {
			v := strings.TrimSpace(it.VariantID)
			variantID = &v
		}

		flavourIDs := make([]string, 0, len(it.Flavours))
		for _, f := range it.Flavours {
			if strings.TrimSpace(f.ID) != "" {
				flavourIDs = append(flavourIDs, strings.TrimSpace(f.ID))
			}
		}

		addonIDs := make([]string, 0, len(it.Addons))
		for _, a := range it.Addons {
			if strings.TrimSpace(a.ID) != "" {
				addonIDs = append(addonIDs, strings.TrimSpace(a.ID))
			}
		}

		if it.UnitPriceCents > 0 {
			lineTotal := it.UnitPriceCents * int64(it.Quantity)
			it.LineTotalCents = lineTotal

			// Calculate item-level discount in cents if percentage is given
			if it.DiscountPct != nil && *it.DiscountPct > 0 {
				it.DiscountCents = int64(math.Round(float64(lineTotal) * (*it.DiscountPct) / 100))
			}
			if it.DiscountCents > lineTotal {
				it.DiscountCents = lineTotal
			}

			// Sync resolved product name and modifiers from the catalog if not provided
			if s.catalog != nil && it.ProductID != "" {
				if priced, err := s.catalog.GetPricedItem(ctx, branchID, it.ProductID, variantID, flavourIDs, addonIDs, nil, nil, time.Now()); err == nil {
					if it.ProductName == "" {
						it.ProductName = priced.ProductName
					}
					if priced.VariantName != nil && it.VariantName == "" {
						it.VariantName = *priced.VariantName
					}
					it.Flavours = syncModifierNames(it.Flavours, priced.SelectedFlavours)
					it.Addons = syncModifierNames(it.Addons, priced.SelectedAddons)
				}
			}
			continue
		}

		priced, err := s.catalog.GetPricedItem(ctx, branchID, it.ProductID, variantID, flavourIDs, addonIDs, nil, nil, time.Now())
		if err != nil {
			return nil, err
		}
		if !priced.Available {
			return nil, apperr.New("PRODUCT_UNAVAILABLE", apperr.CategoryBusinessRule, "This product is currently unavailable: "+priced.AvailableReason)
		}

		it.ProductName = priced.ProductName
		if priced.VariantName != nil {
			it.VariantName = *priced.VariantName
		}
		it.UnitPriceCents = priced.UnitPriceCents
		lineTotal := priced.UnitPriceCents * int64(it.Quantity)
		it.LineTotalCents = lineTotal

		// Calculate item-level discount in cents if percentage is given
		if it.DiscountPct != nil && *it.DiscountPct > 0 {
			it.DiscountCents = int64(math.Round(float64(lineTotal) * (*it.DiscountPct) / 100))
		}
		if it.DiscountCents > lineTotal {
			it.DiscountCents = lineTotal
		}

		// Sync resolved modifier names and prices from the catalog onto the line
		it.Flavours = syncModifierNames(it.Flavours, priced.SelectedFlavours)
		it.Addons = syncModifierNames(it.Addons, priced.SelectedAddons)
	}
	return out, nil
}

// syncModifierNames merges catalog-resolved names and price deltas into the
// client-supplied modifier refs. If the client sent only an id (no name),
// this fills the name and priceDelta from the catalog. If the client sent
// nothing at all, it uses the full catalog list.
func syncModifierNames(refs []ordersapi.SelectedModifierRef, catalogMods []catalogapi.SelectedModifier) []ordersapi.SelectedModifierRef {
	if len(refs) == 0 {
		if len(catalogMods) == 0 {
			return refs
		}
		out := make([]ordersapi.SelectedModifierRef, len(catalogMods))
		for i, m := range catalogMods {
			out[i] = ordersapi.SelectedModifierRef{ID: m.ID, Name: m.Name, PriceDelta: m.PriceDelta}
		}
		return out
	}
	// Build a lookup by id from catalog results.
	byID := make(map[string]catalogapi.SelectedModifier, len(catalogMods))
	for _, m := range catalogMods {
		byID[m.ID] = m
	}
	out := make([]ordersapi.SelectedModifierRef, len(refs))
	for i, r := range refs {
		if r.Name == "" || r.PriceDelta == 0 {
			if cm, ok := byID[r.ID]; ok {
				r.Name = cm.Name
				r.PriceDelta = cm.PriceDelta
			}
		}
		out[i] = r
	}
	return out
}

/*
 * Customer details on a POS update.
 *
 * The till sends them as a customer object (like a new order) or as the
 * guest fields; the object wins, the guest fields fill the gaps.
 */

func hasGuestContact(x *POSOrderExtraFields) bool {
	return x.GuestCustomerName != nil || x.GuestCustomerMobile != nil || x.GuestCustomerEmail != nil
}

// saveCustomerAddressAsSent writes the customer block's address fields onto
// the customer record exactly, blanks included.
func (s *Service) saveCustomerAddressAsSent(ctx context.Context, customerID string, c *POSCustomer) error {
	t := strings.TrimSpace
	return s.store.SetCustomerAddress(ctx, t(customerID), t(c.Address), t(c.Street), t(c.City), t(c.State), t(c.Postcode))
}

// errCustomerExists: the new customer typed on the order is one the store
// already has. The id goes back so the till can offer that customer.
func errCustomerExists(e *store.CustomerExistsError) error {
	what := "mobile number"
	if e.Field == "email" {
		what = "email"
	}
	return apperr.New("CUSTOMER_ALREADY_EXISTS", apperr.CategoryConflict,
		"A customer with this "+what+" already exists. Select the existing customer.").
		WithFields(apperr.FieldError{Field: e.Field, Code: "DUPLICATE", Message: "Already in use."}).
		WithMeta(map[string]any{"existingCustomerId": e.ID, "existingName": e.Name})
}

// customerContactFrom is the name, email and phone to write onto the
// customer record. A nil or blank value leaves the record's own.
// errContactTaken: the email or mobile the till sent is another customer's.
func errContactTaken() error {
	return apperr.New("CUSTOMER_CONTACT_TAKEN", apperr.CategoryConflict,
		"That email or mobile number already belongs to another customer.").
		WithFields(apperr.FieldError{Field: "customer", Code: "TAKEN", Message: "Email or mobile in use."})
}

func customerContactFrom(x *POSOrderExtraFields) (first, last, email, phone *string) {
	if c := x.Customer; c != nil {
		first, last, email, phone = strPtr(c.FirstName), strPtr(c.LastName), strPtr(c.Email), strPtr(c.Phone)
	}
	if first == nil && last == nil && x.GuestCustomerName != nil {
		if parts := strings.Fields(*x.GuestCustomerName); len(parts) > 0 {
			f := parts[0]
			first = &f
			if len(parts) > 1 {
				l := strings.Join(parts[1:], " ")
				last = &l
			}
		}
	}
	if email == nil {
		email = x.GuestCustomerEmail
	}
	if phone == nil {
		phone = x.GuestCustomerMobile
	}
	return first, last, email, phone
}

// fillGuestFromCustomer copies a customer object onto the order's guest
// fields the request left out — the same as a new order does (posOrderShell).
func fillGuestFromCustomer(x *POSOrderExtraFields) {
	c := x.Customer
	if c == nil {
		return
	}
	if x.GuestCustomerName == nil {
		if name := strings.TrimSpace(c.FirstName + " " + c.LastName); name != "" {
			x.GuestCustomerName = &name
		}
	}
	if x.GuestCustomerMobile == nil {
		x.GuestCustomerMobile = strPtr(c.Phone)
	}
	if x.GuestCustomerEmail == nil {
		x.GuestCustomerEmail = strPtr(c.Email)
	}
	if x.GuestCustomerAddress == nil {
		x.GuestCustomerAddress = strPtr(c.Address)
	}
	if x.GuestCustomerCity == nil {
		x.GuestCustomerCity = strPtr(c.City)
	}
	if x.GuestCustomerPostcode == nil {
		x.GuestCustomerPostcode = strPtr(c.Postcode)
	}
}

// strPtr is nil for a blank string.
func strPtr(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}
