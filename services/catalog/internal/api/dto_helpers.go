package api

import (
	"strings"
	"time"

	"bakehouse/pkg/apperr"
)

// adminDateFormats are the shapes a date arrives in.
//
// The console's date inputs are YYYY-MM-DD, but it passes them through
// `new Date(...).toISOString()` before sending, so what actually lands is a
// full RFC 3339 timestamp. Accepting only the first of those is what made
// product scheduling appear to do nothing: the value failed to parse, was
// silently dropped, and the save reported success over a window that had
// never been stored.
var adminDateFormats = []string{
	"2006-01-02",
	"2006-01-02T15:04",
	"2006-01-02T15:04:05",
	time.RFC3339,
	time.RFC3339Nano,
	"2006-01-02T15:04:05.000Z07:00",
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02T15:04Z07:00",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
}

// parseAdminDateStrict reads a date, or explains why it could not.
//
// Empty means "not set", which is a real state — a product with no window is
// always on sale. Anything else that will not parse is an error rather than a
// silent nil: a date the caller meant and the server discarded is the worst of
// the three outcomes, because nothing anywhere says it happened.
func parseAdminDateStrict(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	for _, layout := range adminDateFormats {
		if t, err := time.Parse(layout, value); err == nil {
			day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
			return &day, nil
		}
	}
	location, err := time.LoadLocation("Asia/Kolkata")
	if err == nil {
		if t, err := time.ParseInLocation("2006-01-02T15:04", value, location); err == nil {
			day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
			return &day, nil
		}
	}
	return nil, apperr.Validation(value + " is not a date/datetime. Use YYYY-MM-DD or an ISO 8601 timestamp.").
		WithFields(apperr.FieldError{Field: "date", Code: "INVALID", Message: "Use YYYY-MM-DD or ISO 8601."})
}

func formatDateTime(value string) (string, error) {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return "", err
	}

	return t.Format("2006-01-02T15:04"), nil
}

// parseAdminDate is the forgiving form, for callers that have no way to report
// an error. Prefer parseAdminDateStrict wherever the caller can return one.
func parseAdminDate(value string) *time.Time {
	t, _ := parseAdminDateStrict(value)
	return t
}

func parseAdminDatePtr(value *string) *time.Time {
	if value == nil {
		return nil
	}
	return parseAdminDate(*value)
}

/*
 * A scheduled window carries a time, not just a day.
 *
 * The column holds a timestamp and the parser has always accepted one, but
 * this wrote back only the date — so an admin who scheduled a product to go
 * live at 9am saw "9am" accepted, saved, and then replaced by midnight the
 * next time the form loaded. The time was in the database the whole while;
 * only this could not say so.
 */
func formatDatePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	// Use RFC3339 base string and format to desired layout via formatDateTime
	if ft, err := formatDateTime(t.Format(time.RFC3339)); err == nil {
		return &ft
	}
	// Fallback to direct formatting without timezone
	ft := t.Format("2006-01-02T15:04")
	return &ft
}
