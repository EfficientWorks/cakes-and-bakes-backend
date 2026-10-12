package domain

import "testing"

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Cakes & Bakes Ltd": "cakes-bakes-ltd",
		"  Bake  House  ":   "bake-house",
		"--Café 42--":       "caf-42",
		"!!!":               "",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidateSettings(t *testing.T) {
	if err := validateSettings("GBP", "Europe/London", "inclusive", 20); err != nil {
		t.Fatalf("UK defaults rejected: %v", err)
	}
	for _, c := range []struct {
		currency, tz, mode string
		rate               float64
	}{
		{"GB", "Europe/London", "inclusive", 20},
		{"GBP", "Mars/Olympus", "inclusive", 20},
		{"GBP", "Europe/London", "sometimes", 20},
		{"GBP", "Europe/London", "inclusive", 120},
	} {
		if err := validateSettings(c.currency, c.tz, c.mode, c.rate); err == nil {
			t.Errorf("accepted %+v", c)
		}
	}
}
