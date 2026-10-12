package order

import "strings"

// PrepMinutes converts a catalogue preparation time to minutes.
func PrepMinutes(value int, unit string) int {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(unit)), "hour") {
		return value * 60
	}
	return value
}
