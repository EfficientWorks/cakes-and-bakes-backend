package domain

import (
	"testing"
	"time"

	"bakehouse/services/catalog/catalogapi"
)

// A window longer than the slot listing (12 Oct – 17 Dec 23:59 against a
// month of slots) lists every remaining blocked day, and stops after it.
func TestRestrictedDaysAfterCoversWholeWindow(t *testing.T) {
	rs := []catalogapi.TagRestriction{{
		From: time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2026, 12, 17, 23, 59, 0, 0, time.UTC),
	}}
	got := restrictedDaysAfter(rs, time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC))
	if len(got) != 40 || got[0] != "2026-11-08" || got[len(got)-1] != "2026-12-17" {
		t.Fatalf("got %d days %v … %v", len(got), got[0], got[len(got)-1])
	}
}
