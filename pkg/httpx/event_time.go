package httpx

import (
	"net/http"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/timex"
)

// WithEventTime parses an optional "when did this happen" request field
// (RFC3339 or "2006-01-02 15:04:05") and returns a request whose context
// carries it, so the store stamps the row with it instead of NOW().
// A missing or empty field leaves the request as it was; an unparseable
// one is a 400 naming the field.
func WithEventTime(r *http.Request, field string, raw *string) (*http.Request, error) {
	t, ok := timex.ParseEventTime(raw)
	if !ok {
		return r, apperr.Validation(field + " must be an RFC3339 date-time, e.g. 2026-09-16T10:33:52Z.").
			WithFields(apperr.FieldError{Field: field, Code: "INVALID", Message: "Not a date-time."})
	}
	if t == nil {
		return r, nil
	}
	return r.WithContext(timex.WithEventTime(r.Context(), t)), nil
}
