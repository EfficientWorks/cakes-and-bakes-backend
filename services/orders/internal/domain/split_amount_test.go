package domain

import "testing"

// Three ways of £81.40 is 27.13 / 27.13 / 27.14: every share is rounded
// down and the odd penny rides on the last one, so the shares add up to the
// order and a fully paid split settles it exactly.
func TestSplitByAmountEqualSharesAddUp(t *testing.T) {
	bills, err := splitByAmount(8140, "GBP", SplitRequest{Ways: 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(bills) != 3 {
		t.Fatalf("want 3 bills, got %d", len(bills))
	}
	want := []int64{2713, 2713, 2714}
	var sum int64
	for i, b := range bills {
		if b.AmountCents != want[i] {
			t.Errorf("bill %d: want %d, got %d", i+1, want[i], b.AmountCents)
		}
		sum += b.AmountCents
	}
	if sum != 8140 {
		t.Errorf("shares sum to %d, want 8140", sum)
	}
}

func TestSplitByAmountExactDivision(t *testing.T) {
	bills, err := splitByAmount(1000, "GBP", SplitRequest{Ways: 4})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, b := range bills {
		if b.AmountCents != 250 {
			t.Errorf("want 250, got %d", b.AmountCents)
		}
	}
}
