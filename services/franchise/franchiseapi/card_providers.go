package franchiseapi

import (
	"fmt"
	"strings"
)

// Card providers a store can take payment through. Each also needs its keys
// on the server before it is actually offered (see the payments module).
const CardProviderStripe = "stripe"

// DefaultCardProviders is what a store takes until admin says otherwise:
// Stripe, which every store used before the switch existed.
var DefaultCardProviders = []string{CardProviderStripe}

// NormalizeCardProviders lower-cases, de-duplicates and checks a store's
// card providers. An empty list is allowed: the store then takes card only on
// its own terminal (manual card) and cash.
func NormalizeCardProviders(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, p := range in {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" || seen[p] {
			continue
		}
		if p != CardProviderStripe {
			return nil, fmt.Errorf("cardProviders: %q is not a card provider (stripe)", p)
		}
		seen[p] = true
		out = append(out, p)
	}
	return out, nil
}

// TakesCardThrough reports whether the store has a provider switched on.
func (b *Branch) TakesCardThrough(provider string) bool {
	for _, p := range b.CardProviders {
		if p == provider {
			return true
		}
	}
	return false
}
