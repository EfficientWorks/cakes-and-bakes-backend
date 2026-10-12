package invoice

import "bakehouse/services/admin/adminapi"

func orEmptyModifiers(m []adminapi.InvoiceItemModifier) []adminapi.InvoiceItemModifier {
	if m == nil {
		return []adminapi.InvoiceItemModifier{}
	}
	return m
}
