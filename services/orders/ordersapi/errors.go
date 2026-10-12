package ordersapi

import "errors"

// Sentinel errors the store returns and the domain layer branches on. They
// live in the contract package so both sides can reference them without an
// import cycle.
var (
	// ErrNotFound is returned when a cart, order or slot does not exist.
	ErrNotFound = errors.New("not found")
	// ErrSlotFull is returned when a slot's capacity was exhausted between
	// the pre-check and the reserving transaction (SCH-04).
	ErrSlotFull = errors.New("slot full")
	// ErrIdempotentReplay signals that an order with the supplied
	// Idempotency-Key already exists, so the original must be returned
	// rather than a second order created (SF-28).
	ErrIdempotentReplay = errors.New("idempotent replay")
)
