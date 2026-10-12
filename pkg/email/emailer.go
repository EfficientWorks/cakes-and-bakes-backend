// Package notifications sends transactional email (Postmark) and will grow
// SMS (Twilio, P2) and push notifications later. Every trigger point across
// the codebase depends on the Emailer interface, never on Postmark directly,
// so tests and local dev can swap in a logger.
package email

import "context"

// Email is one transactional message to send.
type Email struct {
	To       string
	Subject  string
	Template string                 // Postmark template alias, e.g. "account-verify-otp"
	Data     map[string]interface{} // merge variables
}

// Emailer sends transactional email. NOT-06: transactional messages are
// never optional/suppressible by the customer — only marketing mail is.
type Emailer interface {
	Send(ctx context.Context, email Email) error
}

// Common template aliases used across modules.
const (
	TemplateAccountVerifyOTP  = "account-verify-otp"
	TemplatePasswordResetOTP  = "password-reset-otp"
	TemplateCustomerWelcome   = "customer-welcome"
	TemplateOrderConfirmation = "order-confirmation"
	TemplateOrderAmended      = "order-amended"
	TemplateOrderCancelled    = "order-cancelled"
	// Sent to the person who filled in the form, so they know it arrived.
	TemplateEnquiryAcknowledged = "enquiry-acknowledged"
	TemplateOrderReady          = "order-ready-pickup"
	TemplateOrderDelivered      = "order-delivered"
	TemplateSlotCancelled       = "slot-cancelled"
	TemplateBranchNewOrder      = "branch-new-order"
	TemplateStaffInvite         = "staff-invite"
	// TemplateEnquiryReceived notifies the handling branch, or head office for
	// brand-level enquiries, that a storefront form was submitted (SF-34).
	TemplateEnquiryReceived = "enquiry-received"
	// TemplateInvoiceIssued sends a raised invoice to whoever it bills — a
	// customer, or another outlet.
	TemplateInvoiceIssued = "invoice-issued"

	// TemplatePaymentLink sends a customer the hosted card payment page for
	// an order taken at the counter (Stripe).
	TemplatePaymentLink = "payment-link"
)
