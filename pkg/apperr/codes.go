package apperr

// Error codes are single-sourced here rather than written inline at the
// throw site. A client switches on these strings, so a typo at one call
// site is a silent API change; a constant is a compile error.
//
// Adding a code: put it in the group it belongs to, give it the category
// that fixes its HTTP status, and add it to the error catalogue in
// docs/api/.
const (
	// --- Cross-cutting ---------------------------------------------------

	CodeValidationFailed    = "VALIDATION_FAILED"
	CodeUnauthenticated     = "UNAUTHENTICATED"
	CodePermissionDenied    = "PERMISSION_DENIED"
	CodeNotFound            = "NOT_FOUND"
	CodeConflict            = "CONFLICT"
	CodeRateLimited         = "RATE_LIMITED"
	CodeInternal            = "INTERNAL_ERROR"
	CodeIdempotencyConflict = "IDEMPOTENCY_CONFLICT"

	// --- Identity --------------------------------------------------------

	CodeEmailInUse         = "EMAIL_IN_USE"
	CodeInvalidCredentials = "INVALID_CREDENTIALS"
	CodeAccountInactive    = "ACCOUNT_INACTIVE"
	CodeOTPInvalid         = "OTP_INVALID"
	CodeOTPExpired         = "OTP_EXPIRED"
	CodeTokenExpired       = "TOKEN_EXPIRED"
	CodeTokenInvalid       = "TOKEN_INVALID"
	CodeEmailSendFailed    = "EMAIL_SEND_FAILED"

	// --- Tenancy and governance ------------------------------------------

	CodeBranchSuspended     = "BRANCH_SUSPENDED"
	CodeFranchiseSuspended  = "FRANCHISE_SUSPENDED"
	CodeDeliveryZoneOverlap = "DELIVERY_ZONE_OVERLAP"
	CodeGovernanceViolation = "GOVERNANCE_VIOLATION"

	// --- Catalogue -------------------------------------------------------

	CodeProductUnavailable = "PRODUCT_UNAVAILABLE"
	CodeModifierInvalid    = "MODIFIER_INVALID"
	CodeVariantInvalid     = "VARIANT_INVALID"

	// --- Scheduling ------------------------------------------------------

	CodeSlotCapacityExceeded  = "SLOT_CAPACITY_EXCEEDED"
	CodeSlotNoLongerAvailable = "SLOT_NO_LONGER_AVAILABLE"
	CodeSlotPastCutoff        = "SLOT_PAST_CUTOFF"
	CodeBranchClosed          = "BRANCH_CLOSED"

	// --- Orders ----------------------------------------------------------

	CodeCartEmpty                = "CART_EMPTY"
	CodeMinimumOrderNotMet       = "MINIMUM_ORDER_NOT_MET"
	CodeDeliveryNotAvailable     = "DELIVERY_NOT_AVAILABLE"
	CodeDeliveryAddressOutOfZone = "DELIVERY_ADDRESS_OUT_OF_ZONE"
	CodeAmendmentWindowClosed    = "AMENDMENT_WINDOW_CLOSED"
	CodeOrderNotCancellable      = "ORDER_NOT_CANCELLABLE"

	// --- Cash drawer -------------------------------------------------------

	CodeCashPayoutExceedsAvailable = "CASH_PAYOUT_EXCEEDS_AVAILABLE"
	CodeCashDayAlreadyClosed       = "CASH_DAY_ALREADY_CLOSED"

	// --- Payments --------------------------------------------------------

	CodePaymentFailed         = "PAYMENT_FAILED"
	CodePaymentsNotConfigured = "PAYMENTS_NOT_CONFIGURED"
	CodeRefundExceedsCapture  = "REFUND_EXCEEDS_CAPTURE"
	CodeWebhookNotConfigured  = "WEBHOOK_NOT_CONFIGURED"
	CodeInvalidSignature      = "INVALID_SIGNATURE"
)
