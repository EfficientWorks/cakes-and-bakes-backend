package domain

import (
	"testing"

	"bakehouse/services/orders/ordersapi"
)

func f64(v float64) *float64 { return &v }
func i64(v int64) *int64     { return &v }

// line is one returned line of qty units at unit cents, from a line of
// lineQty units sold with lineDisc cents off the whole line.
func line(id string, qty int, unit int64, lineQty int, lineDisc int64) ordersapi.ReturnLineInput {
	return ordersapi.ReturnLineInput{OrderItemID: id, Quantity: qty, UnitPriceCents: unit,
		LineQuantity: lineQty, LineDiscountCents: lineDisc}
}

func eqp(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func sameCharges(got, want ordersapi.ReturnCharges) bool {
	return got.GrossCents == want.GrossCents && got.DiscountCents == want.DiscountCents &&
		got.TaxCents == want.TaxCents && got.RefundCents == want.RefundCents &&
		eqp(got.DiscountPct, want.DiscountPct) && eqp(got.TaxPct, want.TaxPct)
}

// Single-line returns: the same answers the whole-return pricer gave.
func TestReturnPricerSingleLine(t *testing.T) {
	cases := []struct {
		name     string
		req      ordersapi.ReturnRequest
		order    ordersapi.Order
		orderTax *float64
		want     ordersapi.ReturnCharges
	}{
		{
			name: "no discount, no tax: refund is gross",
			want: ordersapi.ReturnCharges{GrossCents: 1000, RefundCents: 1000},
		},
		{
			name:     "follows the order's discount rate and tax rate",
			order:    ordersapi.Order{DiscountPct: f64(10), SubtotalCents: 5000},
			orderTax: f64(5),
			// discount 100, taxable 900, tax 45, refund 945
			want: ordersapi.ReturnCharges{GrossCents: 1000, DiscountPct: f64(10), DiscountCents: 100, TaxPct: f64(5), TaxCents: 45, RefundCents: 945},
		},
		{
			name:  "fixed order discount is shared by value",
			order: ordersapi.Order{DiscountCents: 500, SubtotalCents: 5000},
			// 1000 * 500/5000 = 100 → 10%
			want: ordersapi.ReturnCharges{GrossCents: 1000, DiscountPct: f64(10), DiscountCents: 100, RefundCents: 900},
		},
		{
			name:     "till's percentages win over the order's",
			req:      ordersapi.ReturnRequest{DiscountPct: f64(20), TaxPct: f64(10)},
			order:    ordersapi.Order{DiscountPct: f64(10)},
			orderTax: f64(5),
			want:     ordersapi.ReturnCharges{GrossCents: 1000, DiscountPct: f64(20), DiscountCents: 200, TaxPct: f64(10), TaxCents: 80, RefundCents: 880},
		},
		{
			name: "till's cents win over percentages",
			req:  ordersapi.ReturnRequest{DiscountCents: i64(150), TaxCents: i64(50)},
			want: ordersapi.ReturnCharges{GrossCents: 1000, DiscountPct: f64(15), DiscountCents: 150, TaxPct: f64(5.882), TaxCents: 50, RefundCents: 900},
		},
		{
			name: "discount is capped at gross",
			req:  ordersapi.ReturnRequest{DiscountCents: i64(5000)},
			want: ordersapi.ReturnCharges{GrossCents: 1000, DiscountPct: f64(100), DiscountCents: 1000, RefundCents: 0},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lines, total := returnPricer(tc.req, &tc.order, tc.orderTax, nil)([]ordersapi.ReturnLineInput{line("a", 1, 1000, 1, 0)})
			if !sameCharges(total, tc.want) || !sameCharges(lines[0].ReturnCharges, tc.want) {
				t.Errorf("total %+v line %+v, want %+v", total, lines[0].ReturnCharges, tc.want)
			}
		})
	}
}

// The reason for per-item pricing: a line with its own discount is refunded
// net of that discount, prorated by the quantity returned, and is taxed on
// what is left; a line without one is not touched by it.
func TestReturnPricerPerItem(t *testing.T) {
	order := ordersapi.Order{}
	tax := f64(10)
	lines, total := returnPricer(ordersapi.ReturnRequest{}, &order, tax, nil)([]ordersapi.ReturnLineInput{
		// 1 of 4 cupcake boxes at $20, the line had $8 off: $2 belongs to this unit.
		line("box", 1, 2000, 4, 800),
		// 2 macarons at $5, no line discount.
		line("mac", 2, 500, 2, 0),
	})
	wantBox := ordersapi.ReturnCharges{GrossCents: 2000, DiscountCents: 200, DiscountPct: f64(10), TaxCents: 180, TaxPct: f64(10), RefundCents: 1980}
	wantMac := ordersapi.ReturnCharges{GrossCents: 1000, TaxCents: 100, TaxPct: f64(10), RefundCents: 1100}
	if !sameCharges(lines[0].ReturnCharges, wantBox) {
		t.Errorf("box: got %+v want %+v", lines[0].ReturnCharges, wantBox)
	}
	if !sameCharges(lines[1].ReturnCharges, wantMac) {
		t.Errorf("macarons: got %+v want %+v", lines[1].ReturnCharges, wantMac)
	}
	// Totals are the sums of the lines.
	if total.GrossCents != 3000 || total.DiscountCents != 200 || total.TaxCents != 280 || total.RefundCents != 3080 {
		t.Errorf("total: %+v", total)
	}
	if lines[0].OrderItemID != "box" || lines[1].Quantity != 2 {
		t.Errorf("line identity lost: %+v", lines)
	}
}

func TestReturnPricerItemAndOrderDiscount(t *testing.T) {
	// Order has 10% off on top of the line's own discount: 10% of what is
	// left after the line discount.
	order := ordersapi.Order{DiscountPct: f64(10)}
	lines, _ := returnPricer(ordersapi.ReturnRequest{}, &order, nil, nil)([]ordersapi.ReturnLineInput{line("box", 2, 1000, 2, 400)})
	// gross 2000, own 400, net 1600, order share 160 → discount 560 (28%)
	want := ordersapi.ReturnCharges{GrossCents: 2000, DiscountCents: 560, DiscountPct: f64(28), RefundCents: 1440}
	if !sameCharges(lines[0].ReturnCharges, want) {
		t.Errorf("got %+v want %+v", lines[0].ReturnCharges, want)
	}
}

func TestReturnPricerSharesTillCentsAcrossLines(t *testing.T) {
	// The till says $1.00 off and $0.50 tax for the whole return: shared by
	// value, adding up exactly.
	lines, total := returnPricer(ordersapi.ReturnRequest{DiscountCents: i64(100), TaxCents: i64(50)}, &ordersapi.Order{}, nil, nil)(
		[]ordersapi.ReturnLineInput{line("a", 1, 1000, 1, 0), line("b", 1, 2000, 1, 0), line("c", 1, 333, 1, 0)})
	var disc, tax int64
	for _, l := range lines {
		disc += l.DiscountCents
		tax += l.TaxCents
	}
	if disc != 100 || tax != 50 || total.DiscountCents != 100 || total.TaxCents != 50 {
		t.Errorf("shares don't add up: disc %d tax %d total %+v", disc, tax, total)
	}
	if lines[1].DiscountCents <= lines[0].DiscountCents {
		t.Errorf("the dearer line should carry more of the discount: %+v", lines)
	}
}

func TestShareByWeight(t *testing.T) {
	got := shareByWeight(10, []int64{1, 1, 1})
	if got[0]+got[1]+got[2] != 10 {
		t.Fatalf("sum %v", got)
	}
	if z := shareByWeight(10, []int64{0, 0}); z[0] != 0 || z[1] != 0 {
		t.Fatalf("zero weights: %v", z)
	}
}

func deref(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}
