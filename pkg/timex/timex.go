// Package timex holds the date formats and parsing helpers shared across
// services. Every timestamp this backend stores is timestamptz (UTC); the
// date-only values here are branch-local trading dates, formatted with the
// branch's own timezone by the caller.
package timex

import "time"

// DateLayout is the wire format for every date-only value in the API
// (api/conventions.md: "All dates: YYYY-MM-DD").
const DateLayout = "2006-01-02"

// ParseDate parses a YYYY-MM-DD date-only value.
func ParseDate(s string) (time.Time, error) { return time.Parse(DateLayout, s) }

// ParseDatePtr parses a YYYY-MM-DD value, returning nil for an empty
// string so optional query filters stay optional.
func ParseDatePtr(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := ParseDate(s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// FormatDate renders a date-only value in the API wire format.
func FormatDate(t time.Time) string { return t.Format(DateLayout) }
