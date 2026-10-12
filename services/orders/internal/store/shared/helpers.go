package shared

import "strings"

func NilIfEmptyStr(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func NilIfEmptyPtr(p *string) *string {
	if p == nil {
		return nil
	}
	t := strings.TrimSpace(*p)
	if t == "" {
		return nil
	}
	return &t
}

func NullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// TrimPtr trims a set value but keeps an explicit "" (clearing a field),
// unlike NilIfEmptyPtr.
func TrimPtr(p *string) *string {
	if p == nil {
		return nil
	}
	v := strings.TrimSpace(*p)
	return &v
}

// DerefTrim is the trimmed value, or "" for nil.
func DerefTrim(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}
