package branch

import (
	"encoding/json"

	"bakehouse/services/franchise/franchiseapi"
)

// hoursJSON stores a week of hours, or NULL when none was given (the store
// then runs on its opening hours).
func hoursJSON(h []franchiseapi.DayHours) any {
	if len(h) == 0 {
		return nil
	}
	b, _ := json.Marshal(h)
	return b
}

// cardProvidersOrDefault: a store created without saying takes Stripe, as
// every store did before the switch existed.
func cardProvidersOrDefault(p []string) []string {
	if p == nil {
		return franchiseapi.DefaultCardProviders
	}
	return p
}

func join(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += sep
		}
		out += p
	}
	return out
}
