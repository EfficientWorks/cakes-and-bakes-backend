package domain

import "testing"

// The rate is a percentage. Every name it carries says so, and the console
// collects it behind a "%" — so 6.625 has to mean 6.625%, not 662.5%.
func TestTaxIsAPercentage(t *testing.T) {
	pct := func(v float64) *float64 { return &v }

	cases := []struct {
		name    string
		taxable int64
		rate    *float64
		want    int64
	}{
		{"six and a bit percent of $100", 10000, pct(6.625), 663},
		{"six percent of $50", 5000, pct(6), 300},
		{"ten percent of $220", 22000, pct(10), 2200},
		{"zero rate", 10000, pct(0), 0},
		{"no rate configured", 10000, nil, 0},
		{"nothing taxable", 0, pct(6.625), 0},
		{"a credit is not taxed", -500, pct(6.625), 0},
		// Rounded to the nearest cent, not truncated: always rounding down is
		// a shortfall the shop has to make up.
		{"rounds up at the half", 1000, pct(6.65), 67},
		{"rounds down below it", 1000, pct(6.64), 66},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := taxOn(tc.taxable, tc.rate); got != tc.want {
				t.Errorf("taxOn(%d, %v) = %d, want %d", tc.taxable, tc.rate, got, tc.want)
			}
		})
	}
}

// The bug this replaced: a rate multiplied in raw. Worth pinning, because the
// failure was invisible — an order simply came out several times too dear.
func TestTaxDoesNotMultiplyRateRaw(t *testing.T) {
	rate := 6.625
	if got := taxOn(10000, &rate); got > 10000 {
		t.Fatalf("tax of %d on a $100 order — the rate is being applied raw", got)
	}
}
