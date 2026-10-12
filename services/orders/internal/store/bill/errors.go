package bill

import "errors"

// ErrBillsSettled is returned when a split cannot be changed because money
// has already been taken against it.
var ErrBillsSettled = errors.New("this order already has settled bills")

// ErrDuplicatePayment: this provider payment is already recorded on the order
// (a webhook delivered twice).
var ErrDuplicatePayment = errors.New("provider payment already recorded")
