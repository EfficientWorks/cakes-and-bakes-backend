package domain

import (
	"fmt"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/money"
)

/*
 * Minimum order for delivery (F1).
 *
 * A store's minOrderCents is the item subtotal (after item-level prices, before
 * the delivery fee, discount codes and tips) a basket needs before it can be
 * delivered. Below it delivery is unavailable and collection still is. Zero
 * means no minimum.
 */

// deliveryShortfall is how much more the basket needs, or 0.
func deliveryShortfall(minCents, subtotalCents int64) int64 {
	if minCents <= 0 || subtotalCents >= minCents {
		return 0
	}
	return minCents - subtotalCents
}

// deliveryMinimumMessage is the sentence the basket, cart and checkout show:
// "Orders under £50 are collection only. Add £18.01 more to unlock local
// delivery."
func deliveryMinimumMessage(minCents, shortfallCents int64, currency string) string {
	return fmt.Sprintf("Orders under %s are collection only. Add %s more to unlock local delivery.",
		wholeAmount(minCents, currency), money.Format(shortfallCents, currency))
}

// wholeAmount drops ".00" from a round amount: £50 rather than £50.00.
func wholeAmount(minor int64, currency string) string {
	if minor%100 == 0 {
		return money.Symbol(currency) + fmt.Sprint(minor/100)
	}
	return money.Format(minor, currency)
}

// errDeliveryMinimum is checkout's refusal of a delivery below the minimum.
func errDeliveryMinimum(minCents, shortfallCents int64, currency string) error {
	msg := deliveryMinimumMessage(minCents, shortfallCents, currency)
	return apperr.New("DELIVERY_MINIMUM_NOT_MET", apperr.CategoryBusinessRule, msg).
		WithFields(apperr.FieldError{Field: "fulfilmentType", Code: "BELOW_MINIMUM", Message: msg}).
		WithMeta(map[string]any{"deliveryMinimumCents": minCents, "deliveryShortfallCents": shortfallCents})
}
