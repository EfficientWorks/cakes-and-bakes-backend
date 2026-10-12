package api

import (
	"strings"

	"bakehouse/services/orders/ordersapi"
)

// --- Returns: tax and discount -------------------------------------------------

// returnChargesDTOType is the money on a return, or on every return of an
// order: refundAmountCents = grossAmountCents - discountCents + taxCents.
type returnChargesDTOType struct {
	GrossAmountCents  int64    `json:"grossAmountCents"`
	DiscountPct       *float64 `json:"discountPct"`
	DiscountCents     int64    `json:"discountCents"`
	TaxPct            *float64 `json:"taxPct"`
	TaxCents          int64    `json:"taxCents"`
	RefundAmountCents int64    `json:"refundAmountCents"`
}

func returnChargesDTO(c ordersapi.ReturnCharges) returnChargesDTOType {
	return returnChargesDTOType{
		GrossAmountCents: c.GrossCents, DiscountPct: c.DiscountPct, DiscountCents: c.DiscountCents,
		TaxPct: c.TaxPct, TaxCents: c.TaxCents, RefundAmountCents: c.RefundCents,
	}
}

// orderReturnSummaryDTO is the order-based total over every return.
func orderReturnSummaryDTO(o *ordersapi.Order) returnChargesDTOType {
	return returnChargesDTO(ordersapi.ReturnCharges{
		GrossCents: o.ReturnAmountCents, DiscountPct: o.ReturnDiscountPct, DiscountCents: o.ReturnDiscountCents,
		TaxPct: o.ReturnTaxPct, TaxCents: o.ReturnTaxCents, RefundCents: o.ReturnTotalCents,
	})
}

type orderReturnItemDTOType struct {
	OrderItemID string `json:"orderItemId"`
	ProductName string `json:"productName"`
	Quantity    int    `json:"quantity"`
	// Gross: unit price × quantity returned.
	AmountCents int64 `json:"amountCents" example:"2000"`
	// This item's own return discount (its line discount prorated by the
	// quantity returned, plus its share of the order discount) and the tax on
	// what is left. refundAmountCents = amountCents - discountCents + taxCents.
	DiscountPct       *float64 `json:"discountPct" example:"10"`
	DiscountCents     int64    `json:"discountCents" example:"200"`
	TaxPct            *float64 `json:"taxPct" example:"6.625"`
	TaxCents          int64    `json:"taxCents" example:"119"`
	RefundAmountCents int64    `json:"refundAmountCents" example:"1919"`
}

// orderReturnDTOType is one return as recorded (return based).
type orderReturnDTOType struct {
	ID             string  `json:"id"`
	Reason         *string `json:"reason"`
	IsFullReturn   bool    `json:"isFullReturn"`
	ReturnedByID   *string `json:"returnedById"`
	ReturnedByName *string `json:"returnedByName"`
	ReturnedAt     string  `json:"returnedAt"`
	returnChargesDTOType
	Items []orderReturnItemDTOType `json:"items"`
}

func orderReturnDTOList(in []ordersapi.OrderReturn) []orderReturnDTOType {
	out := make([]orderReturnDTOType, 0, len(in))
	for _, r := range in {
		items := make([]orderReturnItemDTOType, 0, len(r.Items))
		for _, it := range r.Items {
			items = append(items, orderReturnItemDTOType{
				OrderItemID: it.OrderItemID, ProductName: it.ProductName,
				Quantity: it.Quantity, AmountCents: it.AmountCents,
				DiscountPct: it.DiscountPct, DiscountCents: it.DiscountCents,
				TaxPct: it.TaxPct, TaxCents: it.TaxCents, RefundAmountCents: it.RefundCents,
			})
		}
		out = append(out, orderReturnDTOType{
			ID: r.ID, Reason: r.Reason, IsFullReturn: r.IsFullReturn,
			ReturnedByID: r.ReturnedBy, ReturnedByName: r.ReturnedByName,
			ReturnedAt:           r.CreatedAt.Format(rfc3339),
			returnChargesDTOType: returnChargesDTO(r.ReturnCharges),
			Items:                items,
		})
	}
	return out
}

// netTotalCents is what an order counts towards a list's revenue total.
func netTotalCents(o *ordersapi.Order) int64 {
	switch strings.ToLower(o.Status) {
	case ordersapi.StatusCancelled, ordersapi.StatusReturned, ordersapi.StatusRefunded:
		return 0
	}
	return o.TotalCents
}
