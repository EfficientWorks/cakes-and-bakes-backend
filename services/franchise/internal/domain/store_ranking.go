package domain

import (
	"context"
	"sort"
	"strings"

	"bakehouse/pkg/geo"
	"bakehouse/services/franchise/franchiseapi"
)

/*
 * Nearest store first.
 *
 * Every store list the storefront shows — the stores that deliver to an
 * address, the pickup list, the "nearest stores" fallback — is ordered by real
 * distance from where the customer is: the straight-line distance between the
 * customer's geocoded address and each store's saved coordinates. ZIP codes
 * are never compared as numbers.
 *
 * The customer is located once per request: their full address, falling back
 * to the ZIP's centre. A store is located from its own address the first time
 * it is needed and the point is saved on the store, so later searches cost no
 * geocoding for it.
 */

// customerPoint is where the customer is, from their address or their ZIP.
func (s *Service) customerPoint(ctx context.Context, address, postcode string) (lat, lng float64, ok bool) {
	address, postcode = strings.TrimSpace(address), strings.TrimSpace(postcode)
	if address == "" && postcode == "" {
		return 0, 0, false
	}
	if lat, lng, ok = geo.LocateAddress(ctx, s.addrGeo, address, "", "", postcode); ok {
		return lat, lng, true
	}
	// No street-address geocoder: the ZIP geocoder, when one is configured.
	if postcode != "" {
		if lat, lng, ok, _ = s.geocoder.Geocode(ctx, postcode); ok {
			return lat, lng, true
		}
	}
	return 0, 0, false
}

// distanceFrom is how far a store is from a point, locating the store first if
// it has no saved coordinates. nil when either end is unknown.
func (s *Service) distanceFrom(ctx context.Context, b *franchiseapi.Branch, lat, lng float64, located bool) *float64 {
	if !located || !s.locateBranch(ctx, b, false) {
		return nil
	}
	d := geo.DistanceMiles(lat, lng, *b.Latitude, *b.Longitude)
	return &d
}

// rankNearest orders stores by distance (miles) from the point, nearest first.
// Stores whose distance is unknown go last; equal distances (to a hundredth of
// a mile) fall back to name, then id, so the order is stable.
func (s *Service) rankNearest(ctx context.Context, branches []franchiseapi.Branch, lat, lng float64, located bool) []franchiseapi.BranchWithDistance {
	out := make([]franchiseapi.BranchWithDistance, 0, len(branches))
	for i := range branches {
		b := branches[i]
		out = append(out, franchiseapi.BranchWithDistance{Branch: b, DistanceMiles: s.distanceFrom(ctx, &b, lat, lng, located)})
		out[len(out)-1].Branch = b // carries coordinates found just now
	}
	sortNearest(out)
	return out
}

func sortNearest(out []franchiseapi.BranchWithDistance) {
	sort.SliceStable(out, func(i, j int) bool {
		di, dj := out[i].DistanceMiles, out[j].DistanceMiles
		switch {
		case di != nil && dj == nil:
			return true
		case di == nil && dj != nil:
			return false
		case di != nil && dj != nil:
			if a, b := roundHundredth(*di), roundHundredth(*dj); a != b {
				return a < b
			}
		}
		if ni, nj := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name); ni != nj {
			return ni < nj
		}
		return out[i].ID < out[j].ID
	})
}

func roundHundredth(v float64) int64 { return int64(v*100 + 0.5) }
