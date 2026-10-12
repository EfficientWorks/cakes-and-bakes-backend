package domain

import (
	"bakehouse/pkg/realtime"
	"bakehouse/pkg/timex"
	"bakehouse/services/orders/internal/store"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/tenant"
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
