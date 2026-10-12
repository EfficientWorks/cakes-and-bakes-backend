// Package strx holds the small string and pointer helpers that every
// service reaches for when mapping between database nulls, JSON optionals
// and domain values. They live here rather than being re-declared per
// package so the null-handling rules stay identical everywhere.
package strx

// OrDefault returns v, or def when v is empty.
func OrDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// NilIfEmpty converts an empty string to a nil *string, so it round-trips
// to SQL NULL and to an omitted JSON field rather than to "".
func NilIfEmpty(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

// Deref returns the pointed-to string, or "" when the pointer is nil.
func Deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Ptr returns a pointer to v. Useful for building optional fields inline.
func Ptr(v string) *string { return &v }

// OrEmptySlice normalises a nil slice to an empty one, so it marshals as
// [] rather than null.
func OrEmptySlice(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}
