package invoice

import (
	"github.com/jackc/pgx/v5"

	"bakehouse/services/admin/adminapi"
)

func scanInvoice(row pgx.Row) (*adminapi.Invoice, error) {
	var (
		inv    adminapi.Invoice
		issued interface{}

		customerFirstName string
		customerLastName  string
		customerPhone     string
		customerEmail     string
		customerAddress   string
		customerCity      string
		customerState     string

		isGift bool

		receiverFirstName string
		receiverLastName  string
		receiverPhone     string
		receiverEmail     string
		receiverAddress   string
		receiverCity      string
		receiverState     string
	)

	err := row.Scan(
		&inv.ID,
		&inv.Reference,
		&inv.Kind,
		&inv.Status,

		&inv.Store.ID,
		&inv.Store.Name,
		&inv.Store.Email,
		&inv.Store.Phone,
		&inv.Store.Street,
		&inv.Store.City,
		&inv.Store.State,
		&inv.Store.Postcode,

		&issued,
		&inv.Message,

		&inv.BankName,
		&inv.AccountNumber,
		&inv.SortCode,

		&inv.SubtotalCents,
		&inv.DiscountCents,
		&inv.DiscountPct,
		&inv.TaxCents,
		&inv.TaxName,
		&inv.TotalCents,

		&inv.PaidAt,
		&inv.SentAt,
		&inv.CreatedAt,

		// billedTo
		&customerFirstName,
		&customerLastName,
		&customerPhone,
		&customerEmail,
		&customerAddress,
		&customerCity,
		&customerState,

		// isGift
		&isGift,

		// receiver
		&receiverFirstName,
		&receiverLastName,
		&receiverPhone,
		&receiverEmail,
		&receiverAddress,
		&receiverCity,
		&receiverState,
	)

	if err != nil {
		return nil, err
	}

	inv.BilledTo = adminapi.InvoiceCustomerInput{
		FirstName: customerFirstName,
		LastName:  customerLastName,
		Phone:     customerPhone,
		Email:     customerEmail,
		Address:   customerAddress,
		City:      customerCity,
		State:     customerState,
	}

	inv.IsGift = isGift

	inv.Receiver = adminapi.InvoiceRecipientInput{
		FirstName: receiverFirstName,
		LastName:  receiverLastName,
		Phone:     receiverPhone,
		Email:     receiverEmail,
		Address:   receiverAddress,
		City:      receiverCity,
		State:     receiverState,
	}

	inv.IssuedDate = formatDateValue(issued)
	inv.Items = []adminapi.InvoiceItem{}

	return &inv, nil
}

// formatDateValue renders whatever the driver hands back for a DATE as
// YYYY-MM-DD. Written defensively because a date column arrives as a
// time.Time here and as a string through some pooled connections.
func formatDateValue(v interface{}) string {
	switch d := v.(type) {
	case string:
		if len(d) >= 10 {
			return d[:10]
		}
		return d
	case interface{ Format(string) string }:
		return d.Format("2006-01-02")
	default:
		return ""
	}
}
