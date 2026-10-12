package shared

import (
	"encoding/json"
	"strings"
)

func NilIfBlank(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

func DecodeStringArray(raw []byte) []string {
	var out []string
	_ = json.Unmarshal(raw, &out)
	return out
}
