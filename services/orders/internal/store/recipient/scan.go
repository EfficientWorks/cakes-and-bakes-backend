package recipient

import (
	"github.com/jackc/pgx/v5"

	"bakehouse/services/orders/ordersapi"
)

func scanStorefrontRecipient(row pgx.Row) (ordersapi.StorefrontRecipient, error) {
	var r ordersapi.StorefrontRecipient
	err := row.Scan(&r.ID, &r.PurchaserEmail, &r.CustomerID, &r.RecipientEmail,
		&r.FirstName, &r.LastName, &r.Phone,
		&r.BusinessName, &r.DeliveryAddressType,
		&r.DeliveryInstructions, &r.DeliveryPostcode, &r.DeliveryState,
		&r.DeliveryCity, &r.DeliveryStreet,
		&r.CreatedAt, &r.UpdatedAt)
	return r, err
}
