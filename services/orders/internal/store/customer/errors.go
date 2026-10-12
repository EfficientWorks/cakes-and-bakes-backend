package customer

import "errors"

// ErrCustomerNotFound is a customer id that names nobody.
var ErrCustomerNotFound = errors.New("customer not found")

// ErrCustomerContactTaken is an email or mobile that already belongs to a
// different customer (both are unique across customers).
var ErrCustomerContactTaken = errors.New("email or mobile already belongs to another customer")

// CustomerExistsError: a new customer typed on an order has the mobile or
// email of a customer this store already has. The till should pick that
// customer instead.
type CustomerExistsError struct {
	ID, Name string
	Field    string // mobileNumber or email
}

func (e *CustomerExistsError) Error() string { return "customer already exists: " + e.ID }
