package domain

import (
	"bakehouse/services/orders/internal/store"
	"context"
	"strings"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/audit"
	"bakehouse/pkg/auth"
)

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
