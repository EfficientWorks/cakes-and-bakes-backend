package domain

import (
	"strings"

	"bakehouse/services/payments/paymentsapi"
)

/*
 * Is there anything at Stripe to refund?
 *
 * Not every order has a real Stripe payment behind it. Orders taken at a
 * counter, seeded for a demo, or placed while Stripe isn't configured carry a
 * placeholder reference — `noop_manual_…`, from paymentsapi.NoopProcessor —
 * and asking Stripe to refund one fails, which would make refunds look broken
 * for every order of that kind. Only a PaymentIntent id (pi_…) is refunded
 * through Stripe; everything else is handed back at the counter.
 */
func refundableAtProvider(paymentID *string) bool {
	if paymentID == nil {
		return false
	}
	return paymentsapi.ProviderOf(strings.TrimSpace(*paymentID)) == paymentsapi.ProviderStripe
}
