package catalogapi

import (
	"testing"
	"time"
)

func wallAt(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, time.UTC)
}

func TestRestrictionOverlaps(t *testing.T) {
	// Mother's Day style: Nov 24 00:30 – Nov 27 13:00, every year.
	r := TagRestriction{From: wallAt(2026, 11, 24, 0, 30), To: wallAt(2026, 11, 27, 13, 0), RepeatYearly: true}
	cases := []struct {
		name       string
		start, end time.Time
		want       bool
	}{
		{"before the window", wallAt(2026, 11, 24, 0, 0), wallAt(2026, 11, 24, 0, 30), false},
		{"first slot inside", wallAt(2026, 11, 24, 0, 30), wallAt(2026, 11, 24, 1, 0), true},
		{"middle day", wallAt(2026, 11, 25, 10, 0), wallAt(2026, 11, 25, 10, 30), true},
		{"last slot inside", wallAt(2026, 11, 27, 12, 30), wallAt(2026, 11, 27, 13, 0), true},
		{"right after", wallAt(2026, 11, 27, 13, 0), wallAt(2026, 11, 27, 13, 30), false},
		{"next year, repeats", wallAt(2027, 11, 25, 9, 0), wallAt(2027, 11, 25, 9, 30), true},
	}
	for _, c := range cases {
		if got := r.Overlaps(c.start, c.end); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	once := r
	once.RepeatYearly = false
	if once.Overlaps(wallAt(2027, 11, 25, 9, 0), wallAt(2027, 11, 25, 9, 30)) {
		t.Error("a one-off window must not repeat")
	}
}

func TestRestrictionOverNewYear(t *testing.T) {
	r := TagRestriction{From: wallAt(2025, 12, 30, 0, 0), To: wallAt(2026, 1, 2, 0, 0), RepeatYearly: true}
	if !r.Overlaps(wallAt(2027, 1, 1, 10, 0), wallAt(2027, 1, 1, 10, 30)) {
		t.Error("Jan 1 2027 should fall in the Dec 30 2026 – Jan 2 2027 occurrence")
	}
	if !r.CoversDay(wallAt(2026, 12, 31, 0, 0)) {
		t.Error("Dec 31 should be covered")
	}
	if r.CoversDay(wallAt(2027, 1, 2, 0, 0)) {
		t.Error("Jan 2 starts when the window ends")
	}
}

func TestFreeAfter(t *testing.T) {
	a := TagRestriction{From: wallAt(2026, 11, 7, 0, 0), To: wallAt(2026, 11, 10, 0, 0)} // 7–9
	b := TagRestriction{From: wallAt(2026, 11, 9, 0, 0), To: wallAt(2026, 11, 11, 0, 0)} // 9–10
	got := FreeAfter(wallAt(2026, 11, 8, 10, 0), []TagRestriction{a, b})
	if !got.Equal(wallAt(2026, 11, 11, 0, 0)) {
		t.Errorf("chained windows: free at %v, want Nov 11", got)
	}
	free := wallAt(2026, 11, 12, 9, 0)
	if !FreeAfter(free, []TagRestriction{a, b}).Equal(free) {
		t.Error("a free time stays put")
	}
}

func TestTagSlug(t *testing.T) {
	for in, want := range map[string]string{"Thank You": "thank-you", "thank_you": "thank-you", "Mother's Day": "mother-s-day", "  Best  Seller ": "best-seller"} {
		if got := TagSlug(in); got != want {
			t.Errorf("TagSlug(%q) = %q, want %q", in, got, want)
		}
	}
}
