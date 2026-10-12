package api

import (
	"bakehouse/services/orders/ordersapi"
)

type billItemDTOType struct {
	OrderItemID string `json:"orderItemId"`
	ProductName string `json:"productName"`
	Quantity    int    `json:"quantity"`
	AmountCents int64  `json:"amountCents"`
}

type billDTOType struct {
	ID          string `json:"id"`
	SplitMode   string `json:"splitMode"`
	Label       string `json:"label"`
	SequenceNo  int    `json:"sequenceNo"`
	AmountCents int64  `json:"amountCents"`
	PaidCents   int64  `json:"paidCents"`
	// What is still owed on this share, so the till does not subtract.
	OutstandingCents int64  `json:"outstandingCents"`
	Status           string `json:"status"`
	// The lines on this bill. Only an item split carries any.
	Items []billItemDTOType `json:"items"`
}

type billPaymentDTOType struct {
	ID            string  `json:"id"`
	BillID        *string `json:"billId"`
	Method        string  `json:"method"`
	AmountCents   int64   `json:"amountCents"`
	TipCents      int64   `json:"tipCents"`
	TenderedCents *int64  `json:"tenderedCents"`
	ChangeCents   int64   `json:"changeCents"`
	Reference     string  `json:"reference"`
	TakenBy       *string `json:"takenBy"`
	CreatedAt     string  `json:"createdAt"`
	// card: stripe (payment link) | manual (own terminal)
	Provider      *string `json:"provider"`
	ProviderRef   *string `json:"providerRef"`
	PaymentLinkID *string `json:"paymentLinkId"`
}

func billDTOList(in []ordersapi.Bill) []billDTOType {
	out := make([]billDTOType, 0, len(in))
	for _, b := range in {
		items := make([]billItemDTOType, 0, len(b.Items))
		for _, it := range b.Items {
			items = append(items, billItemDTOType{
				OrderItemID: it.OrderItemID, ProductName: it.ProductName,
				Quantity: it.Quantity, AmountCents: it.AmountCents,
			})
		}
		outstanding := b.AmountCents - b.PaidCents
		if outstanding < 0 {
			outstanding = 0
		}
		out = append(out, billDTOType{
			ID: b.ID, SplitMode: b.SplitMode, Label: b.Label, SequenceNo: b.SequenceNo,
			AmountCents: b.AmountCents, PaidCents: b.PaidCents, OutstandingCents: outstanding,
			Status: b.Status, Items: items,
		})
	}
	return out
}

func billPaymentDTOList(in []ordersapi.BillPayment) []billPaymentDTOType {
	out := make([]billPaymentDTOType, 0, len(in))
	for _, p := range in {
		out = append(out, billPaymentDTOType{
			ID: p.ID, BillID: p.BillID, Method: p.Method, AmountCents: p.AmountCents,
			TipCents: p.TipCents, TenderedCents: p.TenderedCents, ChangeCents: p.ChangeCents,
			Reference: p.Reference, TakenBy: p.TakenBy, CreatedAt: p.CreatedAt,
			Provider: p.Provider, ProviderRef: p.ProviderRef, PaymentLinkID: p.PaymentLinkID,
		})
	}
	return out
}

type pendingBillDTOType struct {
	ID          string `json:"id"`
	AmountCents int64  `json:"amountCents"`
}

func pendingBillDTOList(in []ordersapi.PendingBillRef) []pendingBillDTOType {
	out := make([]pendingBillDTOType, 0, len(in))
	for _, pb := range in {
		out = append(out, pendingBillDTOType{
			ID:          pb.ID,
			AmountCents: pb.AmountCents,
		})
	}
	return out
}
