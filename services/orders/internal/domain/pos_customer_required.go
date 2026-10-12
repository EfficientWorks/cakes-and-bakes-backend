package domain

import (
	"strings"

	"bakehouse/pkg/apperr"
	"bakehouse/services/orders/ordersapi"
)

/*
 * A pickup or delivery order needs a customer.
 *
 * Somebody has to be called when it is ready or the driver is outside, so
 * the till cannot send or take payment for one without a customer, either an
 * existing record or a name or mobile typed in. Takeaway is paid and handed
 * over at the counter and needs nobody.
 */

func needsCustomer(fulfilment string) bool {
	f := strings.ToLower(strings.TrimSpace(fulfilment))
	return f == ordersapi.FulfilmentPickup || f == ordersapi.FulfilmentDelivery
}

func errCustomerRequired(fulfilment string) error {
	return apperr.Validation("Select a customer for this " + strings.ToLower(fulfilment) + " order.").
		WithFields(apperr.FieldError{Field: "customer", Code: "REQUIRED", Message: "A customer is required for pickup and delivery."})
}

func nonBlank(vals ...*string) bool {
	for _, v := range vals {
		if v != nil && strings.TrimSpace(*v) != "" {
			return true
		}
	}
	return false
}

func (c *POSCustomer) named() bool {
	return c != nil && (strings.TrimSpace(c.FirstName) != "" || strings.TrimSpace(c.Phone) != "")
}

// assertCreateHasCustomer checks a new till order.
func assertCreateHasCustomer(req POSOrderRequest) error {
	if !needsCustomer(req.FulfilmentType) {
		return nil
	}
	if nonBlank(req.CustomerID, req.GuestCustomerName, req.GuestCustomerMobile) || req.Customer.named() {
		return nil
	}
	return errCustomerRequired(req.FulfilmentType)
}

// assertUpdateHasCustomer checks an order after an update: the fulfilment it
// will have, and whether a customer is still on it.
func assertUpdateHasCustomer(order *ordersapi.Order, extra *POSOrderExtraFields) error {
	fulfilment := order.FulfilmentType
	if extra != nil && extra.FulfilmentType != nil && strings.TrimSpace(*extra.FulfilmentType) != "" {
		fulfilment = *extra.FulfilmentType
	}
	if !needsCustomer(fulfilment) {
		return nil
	}
	if extra != nil && (nonBlank(extra.CustomerID, extra.GuestCustomerName, extra.GuestCustomerMobile) || extra.Customer.named()) {
		return nil
	}
	cleared := extra != nil && extra.ClearCustomerID
	if !cleared && (nonBlank(order.CustomerID, order.PurchaserPhone) || strings.TrimSpace(order.PurchaserFirstName) != "") {
		return nil
	}
	return errCustomerRequired(fulfilment)
}
