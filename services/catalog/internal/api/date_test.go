package api

import (
	"testing"
	"time"
)

// The console's date inputs are YYYY-MM-DD, but it sends them through
// new Date(...).toISOString(), so a full RFC 3339 timestamp is what actually
// arrives. Accepting only the first shape is what made product scheduling
// silently do nothing.
func TestParseAdminDateAcceptsWhatTheConsoleSends(t *testing.T) {
	want := time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)

	for _, in := range []string{
		"2026-09-23",
		"2026-09-23T00:00:00.000Z",
		"2026-09-23T00:00:00Z",
		"2026-09-23T12:34:56Z",
		"  2026-09-23  ",
	} {
		got, err := parseAdminDateStrict(in)
		if err != nil {
			t.Errorf("parseAdminDateStrict(%q): %v", in, err)
			continue
		}
		if got == nil || !got.Equal(want) {
			t.Errorf("parseAdminDateStrict(%q) = %v, want %v", in, got, want)
		}
	}
}

// Empty is a real state: a product with no window is always on sale.
func TestEmptyIsNotSetRatherThanAnError(t *testing.T) {
	got, err := parseAdminDateStrict("")
	if err != nil || got != nil {
		t.Errorf("got (%v, %v), want (nil, nil)", got, err)
	}
}

// A date the caller meant and the server discarded is the worst outcome,
// because nothing anywhere says it happened.
func TestAnUnreadableDateIsRefusedRatherThanDropped(t *testing.T) {
	got, err := parseAdminDateStrict("next Tuesday")
	if err == nil {
		t.Fatalf("got (%v, nil), want an error", got)
	}
	if got != nil {
		t.Errorf("a value was returned alongside the error: %v", got)
	}
}

// A timezone must not shift the calendar day. These are DATE columns, and a
// product on sale for one day has to be on sale on that day everywhere.
func TestATimezoneDoesNotShiftTheDay(t *testing.T) {
	got, err := parseAdminDateStrict("2026-09-23T23:30:00+05:30")
	if err != nil {
		t.Fatalf("parseAdminDateStrict: %v", err)
	}
	if got.Day() != 23 || got.Month() != time.September {
		t.Errorf("got %s, want the 23rd of September", got.Format("2006-01-02"))
	}
}

// The patch path must carry the failure out rather than swallowing it.
func TestProductPatchRefusesAnUnreadableDate(t *testing.T) {
	bad := "not a date"
	if _, err := productPatchFromRequest(productPatchRequest{AvailableFrom: &bad}); err == nil {
		t.Error("an unreadable availableFrom was accepted")
	}
}

func TestProductPatchCarriesTheWindowThrough(t *testing.T) {
	from := "2026-09-23T00:00:00.000Z"
	until := "2026-09-25"

	patch, err := productPatchFromRequest(productPatchRequest{AvailableFrom: &from, AvailableUntil: &until})
	if err != nil {
		t.Fatalf("productPatchFromRequest: %v", err)
	}
	if patch.AvailabilityWindow == nil {
		t.Fatal("no window was set")
	}
	if patch.AvailabilityWindow.From == nil || patch.AvailabilityWindow.From.Day() != 23 {
		t.Errorf("from = %v, want the 23rd", patch.AvailabilityWindow.From)
	}
	if patch.AvailabilityWindow.Until == nil || patch.AvailabilityWindow.Until.Day() != 25 {
		t.Errorf("until = %v, want the 25th", patch.AvailabilityWindow.Until)
	}
}
