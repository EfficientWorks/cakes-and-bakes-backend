package api

import (
	"strings"
	"testing"
)

func TestCustomOrderFromRequest(t *testing.T) {
	// An older client with no usable date records the enquiry only.
	if o, err := customOrderFromRequest(customOrderRequest{EventDate: "soon"}, ""); err != nil || o != nil {
		t.Fatalf("no date: %v %v", o, err)
	}
	// An explicit date that is not YYYY-MM-DD is a 400.
	if _, err := customOrderFromRequest(customOrderRequest{Date: "14/11/2026"}, ""); err == nil {
		t.Fatal("bad date accepted")
	}
	// eventDate (either format) and eventTime stand in for date and slot.
	guests := 30
	o, err := customOrderFromRequest(customOrderRequest{
		Name: "Priya Shah", EventDate: "14/11/2026", EventTime: "15:00", EventType: "baby_shower",
		EventLocation: "12 High St", GuestCount: &guests, Flavour: "Vanilla",
		CakeMessage: "Welcome baby", PhotoURLs: []string{"abc"},
	}, "Pastel colours")
	if err != nil || o == nil {
		t.Fatalf("got %v %v", o, err)
	}
	if o.Date.Format("2006-01-02") != "2026-11-14" || o.SlotStart != "15:00" || o.Street != "12 High St" {
		t.Fatalf("date/slot/street = %s %q %q", o.Date.Format("2006-01-02"), o.SlotStart, o.Street)
	}
	if o.Title != "Custom cake" || o.CakeMessage != "Welcome baby" || len(o.PhotoRefs) != 1 {
		t.Fatalf("title/message/photos = %q %q %v", o.Title, o.CakeMessage, o.PhotoRefs)
	}
	for _, want := range []string{"Occasion: Baby shower", "Guests: 30", "Flavour: Vanilla", "Details: Pastel colours", "Price to be confirmed"} {
		if !strings.Contains(o.Notes, want) {
			t.Errorf("notes missing %q:\n%s", want, o.Notes)
		}
	}
}
