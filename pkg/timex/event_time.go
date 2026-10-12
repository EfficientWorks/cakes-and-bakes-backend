package timex

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

/*
 * Event time.
 *
 * A till or console may tell the API WHEN something happened — a payment
 * taken while offline, a status change keyed in after the fact, a day closed
 * at 23:58 but sent at 00:03. Every store that stamps such a moment reads it
 * from the context with EventTimeFrom and falls back to NOW() when there is
 * none, so no handler → domain → store signature had to change to thread it
 * through, and clients that never send it behave exactly as before.
 *
 * Handlers parse the optional request field with ParseEventTime and attach
 * it with WithEventTime. Only the current request sees it.
 */

type eventTimeKey struct{}

// WithEventTime attaches the moment the caller says the action happened.
// A nil time returns ctx unchanged.
func WithEventTime(ctx context.Context, t *time.Time) context.Context {
	if t == nil {
		return ctx
	}
	return context.WithValue(ctx, eventTimeKey{}, *t)
}

// EventTimeFrom returns the caller-supplied moment, or nil when the request
// did not say — in which case the store uses NOW().
func EventTimeFrom(ctx context.Context) *time.Time {
	if t, ok := ctx.Value(eventTimeKey{}).(time.Time); ok {
		return &t
	}
	return nil
}

// EventTimeOr is EventTimeFrom with a Go-side fallback, for stores that
// build the timestamp in Go rather than in SQL.
func EventTimeOr(ctx context.Context, fallback time.Time) time.Time {
	if t := EventTimeFrom(ctx); t != nil {
		return *t
	}
	return fallback
}

// ParseEventTime accepts the layouts a client is likely to send. Empty or
// nil means "not given".
func ParseEventTime(raw *string) (*time.Time, bool) {
	if raw == nil {
		return nil, true
	}
	s := strings.TrimSpace(*raw)
	if s == "" {
		return nil, true
	}
	for _, layout := range []string{
		time.RFC3339Nano, time.RFC3339,
		"2006-01-02T15:04:05", "2006-01-02 15:04:05",
		"2006-01-02T15:04", "2006-01-02 15:04",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return &t, true
		}
	}
	return nil, false
}

// FlexTime is a time.Time that unmarshals from the layouts clients actually
// send (RFC3339, "2006-01-02T15:04", "2006-01-02 15:04:05", …), not only
// RFC3339. A value with no zone is taken as UTC.
type FlexTime time.Time

func (t *FlexTime) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	parsed, ok := ParseEventTime(&s)
	if !ok {
		return fmt.Errorf("%q is not a date-time (use RFC3339, e.g. 2026-09-18T10:53:00Z)", s)
	}
	if parsed == nil {
		return nil // "" → zero value; callers use Ptr() which returns nil
	}
	*t = FlexTime(*parsed)
	return nil
}

func (t FlexTime) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Time(t))
}

// Ptr returns the underlying *time.Time, or nil for a nil/zero FlexTime.
func (t *FlexTime) Ptr() *time.Time {
	if t == nil || time.Time(*t).IsZero() {
		return nil
	}
	tt := time.Time(*t)
	return &tt
}
