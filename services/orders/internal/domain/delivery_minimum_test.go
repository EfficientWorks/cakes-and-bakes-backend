package domain

import (
	"errors"
	"testing"

	"bakehouse/pkg/apperr"
	"bakehouse/services/catalog/catalogapi"
)

func TestDeliveryShortfall(t *testing.T) {
	cases := []struct{ min, sub, want int64 }{
		{0, 100, 0},        // no minimum
		{-5, 100, 0},       // nonsense minimum is none
		{5000, 3199, 1801}, // below
		{5000, 5000, 0},    // exactly at it
		{5000, 7000, 0},    // over
	}
	for _, c := range cases {
		if got := deliveryShortfall(c.min, c.sub); got != c.want {
			t.Errorf("deliveryShortfall(%d, %d) = %d, want %d", c.min, c.sub, got, c.want)
		}
	}
}

func TestDeliveryMinimumMessage(t *testing.T) {
	got := deliveryMinimumMessage(5000, 1801, "GBP")
	want := "Orders under £50 are collection only. Add £18.01 more to unlock local delivery."
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	if got := deliveryMinimumMessage(2550, 50, "GBP"); got != "Orders under £25.50 are collection only. Add £0.50 more to unlock local delivery." {
		t.Fatalf("pence minimum: %q", got)
	}
	var e *apperr.Error
	if err := errDeliveryMinimum(5000, 1801, "GBP"); !errors.As(err, &e) || e.Code != "DELIVERY_MINIMUM_NOT_MET" {
		t.Fatalf("error = %v", err)
	}
}

func errCode(err error) string {
	var e *apperr.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func TestCakeMessageFor(t *testing.T) {
	required := catalogapi.CakeOptions{CakeMessageMode: catalogapi.OptionRequired, CakeMessageMaxLength: 10}
	if _, err := cakeMessageFor(required, nil, true); errCode(err) != "CAKE_MESSAGE_REQUIRED" {
		t.Fatalf("missing: %v", err)
	}
	if _, err := cakeMessageFor(required, nil, false); err != nil {
		t.Fatalf("staff may leave it: %v", err)
	}
	long := "Happy Birthday!"
	if _, err := cakeMessageFor(required, &long, true); errCode(err) != "CAKE_MESSAGE_TOO_LONG" {
		t.Fatalf("long: %v", err)
	}
	off := catalogapi.CakeOptions{}
	if _, err := cakeMessageFor(off, &long, true); errCode(err) != "CAKE_MESSAGE_NOT_ALLOWED" {
		t.Fatalf("off: %v", err)
	}
	padded := "  Hi Amma  "
	got, err := cakeMessageFor(required, &padded, true)
	if err != nil || got == nil || *got != "Hi Amma" {
		t.Fatalf("trimmed: %v %v", got, err)
	}
	if got, err := cakeMessageFor(off, nil, true); err != nil || got != nil {
		t.Fatalf("none: %v %v", got, err)
	}
}

func TestCustomOrderSlot(t *testing.T) {
	cases := []struct {
		start, end, wantS, wantE string
		ok                       bool
	}{
		{"", "", "09:00", "09:30", true},
		{"14:00", "", "14:00", "14:30", true},
		{"14:00:00", "15:00", "14:00", "15:00", true},
		{"14:00", "13:00", "", "", false},
		{"2pm", "", "", "", false},
	}
	for _, c := range cases {
		s, e, err := customOrderSlot(c.start, c.end)
		if (err == nil) != c.ok || s != c.wantS || e != c.wantE {
			t.Errorf("customOrderSlot(%q, %q) = %q, %q, %v", c.start, c.end, s, e, err)
		}
	}
	if f, err := customOrderFulfilment("collection"); err != nil || f != "pickup" {
		t.Fatalf("collection: %q %v", f, err)
	}
	if _, err := customOrderFulfilment("drone"); err == nil {
		t.Fatal("unknown fulfilment accepted")
	}
	if f, l := splitName("Priya Shah Patel"); f != "Priya" || l != "Shah Patel" {
		t.Fatalf("splitName = %q %q", f, l)
	}
}
