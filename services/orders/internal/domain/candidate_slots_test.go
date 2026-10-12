package domain

import (
	"testing"
	"time"
)

func TestCandidateSlots(t *testing.T) {
	loc := time.UTC
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, loc)
	// Earliest bookable 11:30 → the 10:00 and 11:00 slots are skipped.
	earliest := time.Date(2026, 10, 1, 11, 30, 0, 0, loc)
	got := candidateSlots(day, "10:00", "14:00", 60, earliest, loc)
	want := []string{"12:00-13:00", "13:00-14:00"}
	if len(got) != len(want) {
		t.Fatalf("got %d slots, want %d: %+v", len(got), len(want), got)
	}
	for i, k := range got {
		if s := k.SlotStart + "-" + k.SlotEnd; s != want[i] {
			t.Errorf("slot %d = %s, want %s", i, s, want[i])
		}
	}
	if candidateSlots(day, "10:00", "14:00", 0, earliest, loc) != nil {
		t.Error("zero duration must not loop forever or return slots")
	}
}
