package domain

import (
	"bakehouse/services/catalog/catalogapi"
	"testing"
	"time"
)

func mustDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("parse date %q: %v", s, err)
	}
	return d
}

func TestResolveAvailability_MasterCatalogueFallback(t *testing.T) {
	p := catalogapi.Product{IsActive: false}
	rule := catalogapi.BranchProductRule{BranchEnabled: true, AvailableDays: catalogapi.DefaultAvailableDays}
	got := resolveAvailability(p, rule, mustDate(t, "2026-08-25"))
	if got.Available {
		t.Fatal("discontinued product must not be available even if the branch enables it")
	}
	if got.Reason != "Product discontinued" {
		t.Fatalf("unexpected reason: %q", got.Reason)
	}
}

func TestResolveAvailability_BranchDisabled(t *testing.T) {
	p := catalogapi.Product{IsActive: true}
	rule := catalogapi.BranchProductRule{BranchEnabled: false, AvailableDays: catalogapi.DefaultAvailableDays}
	got := resolveAvailability(p, rule, mustDate(t, "2026-08-25"))
	if got.Available {
		t.Fatal("branch-disabled product must not be available")
	}
}

func TestResolveAvailability_DayOfWeekSchedule(t *testing.T) {
	p := catalogapi.Product{IsActive: true}
	rule := catalogapi.BranchProductRule{BranchEnabled: true, AvailableDays: []string{"monday", "tuesday"}}
	// 2026-08-25 is a Tuesday.
	if got := resolveAvailability(p, rule, mustDate(t, "2026-08-25")); !got.Available {
		t.Fatalf("expected available on Tuesday, got: %+v", got)
	}
	// 2026-08-27 is a Thursday — not in the allowed day list.
	if got := resolveAvailability(p, rule, mustDate(t, "2026-08-27")); got.Available {
		t.Fatalf("expected unavailable on Thursday, got: %+v", got)
	}
}

func TestResolveAvailability_SeasonalWindow(t *testing.T) {
	p := catalogapi.Product{IsActive: true}
	from := mustDate(t, "2026-09-01")
	rule := catalogapi.BranchProductRule{BranchEnabled: true, AvailableDays: catalogapi.DefaultAvailableDays, AvailableFrom: &from}

	got := resolveAvailability(p, rule, mustDate(t, "2026-08-25"))
	if got.Available {
		t.Fatal("expected unavailable before the seasonal start date")
	}
	if got.NextAvailableDate == nil || *got.NextAvailableDate != "2026-09-01" {
		t.Fatalf("expected nextAvailableDate 2026-09-01, got %v", got.NextAvailableDate)
	}
}

func TestResolveAvailability_DateOverrideWinsOverEverything(t *testing.T) {
	// Even a discontinued, branch-disabled product becomes available if an
	// explicit date override says so — layer 4 is the highest priority.
	p := catalogapi.Product{IsActive: false}
	rule := catalogapi.BranchProductRule{
		BranchEnabled: false,
		AvailableDays: []string{}, // no days allowed
		DateOverrides: []catalogapi.DateOverride{{Date: "2026-08-25", Available: true, Reason: "Special one-off availability"}},
	}
	got := resolveAvailability(p, rule, mustDate(t, "2026-08-25"))
	if !got.Available {
		t.Fatalf("date override must win over every lower layer, got: %+v", got)
	}
	if got.Reason != "Special one-off availability" {
		t.Fatalf("unexpected reason: %q", got.Reason)
	}
}

func TestResolveAvailability_SoldOutTodayOnly(t *testing.T) {
	p := catalogapi.Product{IsActive: true}
	rule := catalogapi.BranchProductRule{
		BranchEnabled: true,
		AvailableDays: catalogapi.DefaultAvailableDays,
		DateOverrides: []catalogapi.DateOverride{{Date: "2026-08-25", Available: false, Reason: "Sold out for today"}},
	}
	if got := resolveAvailability(p, rule, mustDate(t, "2026-08-25")); got.Available {
		t.Fatal("expected sold out today")
	}
	if got := resolveAvailability(p, rule, mustDate(t, "2026-08-26")); !got.Available {
		t.Fatal("sold-out override must not carry over to the next day")
	}
}
