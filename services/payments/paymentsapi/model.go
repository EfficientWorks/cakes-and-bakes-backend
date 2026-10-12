// Package paymentsapi is the payments service's published contract: the
// payment and refund records it owns, and the Processor seam that orders
// captures money through. It holds no logic and imports nothing from the
// service itself.
package paymentsapi

import "time"

const (
	MethodCard          = "card"
	MethodCash          = "cash"
	MethodApplePay      = "apple_pay"
	MethodGooglePay     = "google_pay"
	MethodTerminal      = "terminal"
	MethodManualCounter = "manual_counter"

	StatusPending       = "pending"
	StatusCaptured      = "captured"
	StatusFailed        = "failed"
	StatusRefunded      = "refunded"
	StatusPartialRefund = "partial_refund"

	RefundStatusPending   = "pending"
	RefundStatusSucceeded = "succeeded"
	RefundStatusFailed    = "failed"
)

type Payment struct {
	ID                      string
	OrderID                 *string
	BranchID                *string
	Method                  string
	AmountCents             int64
	Currency                string
	ProviderPaymentID       *string // Stripe PaymentIntent id (pi_…), or a counter placeholder (noop_…)
	ProviderPaymentMethodID *string // the payment method the intent was created with
	Status                  string
	FailureReason           *string
	CapturedAt              *time.Time
	CreatedAt               time.Time
}

type Refund struct {
	ID               string
	PaymentID        string
	OrderID          string
	AmountCents      int64
	Reason           string
	ProviderRefundID *string
	Status           string
	IssuedByUserID   *string
	ApprovedByUserID *string
	CreatedAt        time.Time
}
