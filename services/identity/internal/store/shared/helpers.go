package shared

import "strings"

func NilIfBlank(v string) *string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return &v
}
