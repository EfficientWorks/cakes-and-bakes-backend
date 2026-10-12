package shared

import "strings"

// NilIfEmpty stores an empty string as NULL.
//
// A cleared text field means "there isn't one", and an empty string in the
// column would read back as a present-but-blank value that every consumer then
// has to special-case.
func NilIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func Deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// TrimmedPtr keeps nil as nil — "not sent" — while tidying anything that was.
func TrimmedPtr(s *string) *string {
	if s == nil {
		return nil
	}
	t := strings.TrimSpace(*s)
	return &t
}
