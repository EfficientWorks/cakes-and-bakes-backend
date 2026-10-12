package api

import (
	"math"
	"strings"
	"time"

	"bakehouse/services/orders/ordersapi"
)

const dateFmt = "2006-01-02"
const rfc3339 = "2006-01-02T15:04:05Z07:00"

type modifierDTOType struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	PriceDelta int64  `json:"priceDelta"`
}

func modDTO(m ordersapi.SelectedModifier) modifierDTOType {
	return modifierDTOType{ID: m.ID, Name: m.Name, PriceDelta: m.PriceDelta}
}
func modDTOList(m []ordersapi.SelectedModifier) []modifierDTOType {
	out := make([]modifierDTOType, 0, len(m))
	for _, x := range m {
		out = append(out, modDTO(x))
	}
	return out
}
func modDTOPtr(m *ordersapi.SelectedModifier) *modifierDTOType {
	if m == nil {
		return nil
	}
	d := modDTO(*m)
	return &d
}

type priceSummaryDTOType struct {
	SubtotalCents      int64   `json:"subtotal"`
	DiscountCents      int64   `json:"discount"`
	DiscountPct        float64 `json:"discountPct"`
	DeliveryFeeCents   int64   `json:"deliveryFee"`
	TaxCents           int64   `json:"tax"`
	TaxPct             float64 `json:"taxPct"`
	TaxName            string  `json:"taxName"`
	ServiceChargeCents int64   `json:"serviceCharge"`
	TipCents           int64   `json:"tip"`
	TotalCents         int64   `json:"total"`
	Currency           string  `json:"currency"`
	// False when the store doesn't deliver to the cart's address (or none is
	// set yet); deliveryMessage then says why. Always true for pickup.
	DeliveryAvailable bool   `json:"deliveryAvailable"`
	DeliveryMessage   string `json:"deliveryMessage,omitempty"`
}

// defaultCurrency is shown for amounts with no currency recorded: the
// product's launch market.
const defaultCurrency = "GBP"

func currencyOr(c string) string {
	if c == "" {
		return defaultCurrency
	}
	return c
}

func priceSummaryDTO(p *ordersapi.PriceSummary) priceSummaryDTOType {
	if p == nil {
		return priceSummaryDTOType{Currency: defaultCurrency}
	}
	var discountPct float64
	if p.DiscountPct != nil {
		discountPct = *p.DiscountPct
	} else if p.SubtotalCents > 0 && p.DiscountCents > 0 {
		discountPct = math.Round(float64(p.DiscountCents)*10000/float64(p.SubtotalCents)) / 100
	}

	var taxPct float64
	if p.TaxPct != nil {
		taxPct = *p.TaxPct
	} else {
		taxable := p.SubtotalCents - p.DiscountCents
		if taxable > 0 && p.TaxCents > 0 {
			taxPct = math.Round(float64(p.TaxCents)*10000/float64(taxable)) / 100
		}
	}

	return priceSummaryDTOType{
		SubtotalCents: p.SubtotalCents, DiscountCents: p.DiscountCents, DiscountPct: discountPct,
		DeliveryFeeCents: p.DeliveryFeeCents, TaxCents: p.TaxCents, TaxPct: taxPct,
		TaxName: taxLabel(p.TaxName), ServiceChargeCents: p.ServiceChargeCents, TipCents: p.TipCents, TotalCents: p.TotalCents, Currency: currencyOr(p.Currency),
		DeliveryAvailable: p.DeliveryAvailable, DeliveryMessage: p.DeliveryMessage,
	}
}

func initialOf(s string) string {
	if s == "" {
		return ""
	}
	return string(s[0]) + "."
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// formatTimePtr renders an optional timestamp, keeping it absent from the JSON
// rather than emitting a zero time — "never collected" and "collected at the
// epoch" must not look the same to a client.
func formatTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(rfc3339)
	return &s
}

// fmtTimePtr renders an optional timestamp, or null when there isn't one.
func fmtTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(rfc3339)
	return &s
}

// taxLabel falls back to something a customer can read. An outlet that has
// never been given a tax name should still not show an empty row.
func taxLabel(name string) string {
	if strings.TrimSpace(name) == "" {
		return "Sales Tax"
	}
	return name
}

// orderStartTime is the stored start_time; created_at only for rows that
// predate the column and were not backfilled.
func orderStartTime(o *ordersapi.Order) string {
	if o.StartTime != nil {
		return o.StartTime.Format(rfc3339)
	}
	return o.CreatedAt.Format(rfc3339)
}

// orderEndTime is the stored end_time, or empty while the order is still
// open — no longer updated_at, which moved on every edit.
func orderEndTime(o *ordersapi.Order) string {
	if o.EndTime != nil {
		return o.EndTime.Format(rfc3339)
	}
	return ""
}

func totalDuration(o *ordersapi.Order) *int {
	if o.TotalDurationSecs != nil {
		return o.TotalDurationSecs
	}
	if o.PaymentStatus == "captured" || o.Status == "fulfilled" || o.Status == "settled" {
		dur := int(o.UpdatedAt.Sub(o.CreatedAt).Seconds())
		if dur < 0 {
			dur = 0
		}
		return &dur
	}
	return nil
}

func settledDisplay(o *ordersapi.Order) *string {
	if o.SettledByName != nil && strings.TrimSpace(*o.SettledByName) != "" {
		return o.SettledByName
	}
	return o.SettledBy
}

func returnedDisplay(o *ordersapi.Order) *string {
	if o.ReturnedByName != nil && strings.TrimSpace(*o.ReturnedByName) != "" {
		return o.ReturnedByName
	}
	return o.ReturnedBy
}

// settleCancelDTO is when an order was settled and, for a cancelled order,
// who cancelled it, when and why. Embedded in the order and list DTOs.
type settleCancelDTO struct {
	// When the order was fully paid. Null while money is owed.
	SettledAt *string `json:"settledAt" example:"2026-09-28T10:45:10Z"`
	// Null unless the order is cancelled.
	CancelledAt *string `json:"cancelledAt" example:"2026-09-28T10:40:00Z"`
	// The staff member's or customer's id; null when the system cancelled it.
	CancelledByID *string `json:"cancelledById"`
	// Their name (email when they have none).
	CancelledByName *string `json:"cancelledByName" example:"Olivia Carter"`
	// staff, customer or system.
	CancelledByType *string `json:"cancelledByType" example:"staff"`
	// Name for display: cancelledByName, else "System" / "Customer".
	CancelledBy  *string `json:"cancelledBy" example:"Olivia Carter"`
	CancelReason *string `json:"cancelReason" example:"Customer changed their mind"`
}

func settleCancel(o *ordersapi.Order) settleCancelDTO {
	out := settleCancelDTO{SettledAt: fmtTimePtr(o.SettledAt)}
	c := o.Cancellation
	if c == nil {
		return out
	}
	// Read back from JSON in UTC; shown in the same zone as every other
	// timestamp on the order.
	at := c.At.In(time.Local).Format(rfc3339)
	out.CancelledAt = &at
	out.CancelledByID = c.ByID
	out.CancelledByName = c.ByName
	if c.ByType != "" {
		t := c.ByType
		out.CancelledByType = &t
	}
	display := c.ByName
	if display == nil || strings.TrimSpace(*display) == "" {
		var d string
		switch c.ByType {
		case "customer":
			d = "Customer"
		case "staff":
			d = "Staff"
		default:
			d = "System"
		}
		display = &d
	}
	out.CancelledBy = display
	if c.Reason != nil && strings.TrimSpace(*c.Reason) != "" {
		out.CancelReason = c.Reason
	}
	return out
}
