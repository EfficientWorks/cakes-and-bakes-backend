package money

import "testing"

func TestFormat(t *testing.T) {
	cases := []struct {
		minor    int64
		currency string
		want     string
	}{
		{1250, "GBP", "£12.50"},
		{1250, "gbp", "£12.50"},
		{5, "GBP", "£0.05"},
		{0, "USD", "$0.00"},
		{-1999, "GBP", "-£19.99"},
		{100000, "INR", "₹1000.00"},
		{1250, "CHF", "CHF 12.50"},
		{1250, "", "12.50"},
	}
	for _, c := range cases {
		if got := Format(c.minor, c.currency); got != c.want {
			t.Errorf("Format(%d, %q) = %q, want %q", c.minor, c.currency, got, c.want)
		}
	}
}

func TestSymbol(t *testing.T) {
	if Symbol("GBP") != "£" || Symbol("CHF") != "CHF" {
		t.Fatal("symbol lookup is wrong")
	}
}
