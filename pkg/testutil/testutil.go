// Package testutil holds the helpers shared by tests across services.
//
// Conventions call for table-driven tests; these helpers exist so a table
// row stays a statement about behaviour rather than ten lines of scaffolding.
// Nothing here is imported by production code.
package testutil

import (
	"errors"
	"testing"
	"time"

	"bakehouse/pkg/apperr"
)

// RequireNoError fails the test if err is non-nil.
func RequireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// RequireErrorCode asserts that err is an *apperr.Error carrying wantCode.
// Tests assert on the code, never on the message: the code is the API
// contract, the message is copy that product can reword.
func RequireErrorCode(t *testing.T, err error, wantCode string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error %s, got nil", wantCode)
	}
	appErr, ok := apperr.As(err)
	if !ok {
		t.Fatalf("expected *apperr.Error %s, got %T: %v", wantCode, err, err)
	}
	if appErr.Code != wantCode {
		t.Fatalf("expected error code %s, got %s (%v)", wantCode, appErr.Code, err)
	}
}

// RequireCategory asserts the error's category, which fixes its HTTP status.
func RequireCategory(t *testing.T, err error, want apperr.Category) {
	t.Helper()
	appErr, ok := apperr.As(err)
	if !ok {
		t.Fatalf("expected *apperr.Error, got %T: %v", err, err)
	}
	if appErr.Category != want {
		t.Fatalf("expected category %s, got %s", want, appErr.Category)
	}
}

// RequireIs asserts errors.Is(err, target).
func RequireIs(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("expected error to wrap %v, got %v", target, err)
	}
}

// RequireEqual compares two comparable values.
func RequireEqual[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// Ptr returns a pointer to v. Table rows are full of optional fields; this
// keeps them on one line.
func Ptr[T any](v T) *T { return &v }

// Date builds a UTC date-only time, the form branch-local trading dates
// take once parsed.
func Date(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// Clock is a fixed clock for logic that reads the current time. Anything
// depending on time.Now directly cannot be tested at a slot cut-off
// boundary, which is exactly where the interesting bugs are.
type Clock struct {
	now time.Time
}

// NewClock returns a Clock frozen at now.
func NewClock(now time.Time) *Clock { return &Clock{now: now} }

// Now returns the frozen time.
func (c *Clock) Now() time.Time { return c.now }

// Advance moves the clock forward by d.
func (c *Clock) Advance(d time.Duration) { c.now = c.now.Add(d) }

// Set moves the clock to an absolute time.
func (c *Clock) Set(t time.Time) { c.now = t }
