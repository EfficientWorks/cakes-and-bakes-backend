package geo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// AddressGeocoder resolves a full US street address (or just a ZIP) to
// coordinates. Radius delivery needs one for both ends: the store and the
// customer. ok=false means the address could not be found, which callers
// treat as "can't deliver there" rather than as an error.
type AddressGeocoder interface {
	GeocodeAddress(ctx context.Context, address string) (lat, lng float64, ok bool, err error)
}

// NoopAddressGeocoder never finds anything; used when LocationIQ is not
// configured. Radius stores then can't be quoted, ZIP stores are unaffected.
type NoopAddressGeocoder struct{}

func (NoopAddressGeocoder) GeocodeAddress(context.Context, string) (float64, float64, bool, error) {
	return 0, 0, false, nil
}

// LocationIQGeocoder calls LocationIQ forward geocoding, US only, and caches
// answers in memory: the same address is priced many times while a customer
// moves through checkout.
type LocationIQGeocoder struct {
	token string
	base  string
	http  *http.Client

	mu    sync.Mutex
	cache map[string]cachedPoint
}

type cachedPoint struct {
	lat, lng float64
	ok       bool
	at       time.Time
}

const (
	geocodeCacheTTL = 24 * time.Hour
	geocodeCacheMax = 5000
)

func NewLocationIQGeocoder(token string) *LocationIQGeocoder {
	return &LocationIQGeocoder{
		token: token, base: "https://us1.locationiq.com/v1/search",
		http: &http.Client{Timeout: 6 * time.Second}, cache: map[string]cachedPoint{},
	}
}

func (g *LocationIQGeocoder) GeocodeAddress(ctx context.Context, address string) (float64, float64, bool, error) {
	norm := strings.ToLower(strings.Join(strings.Fields(address), " "))
	if norm == "" {
		return 0, 0, false, nil
	}
	// Per country: the same text is a different place in another one.
	key := CountryOf(ctx) + "|" + norm
	g.mu.Lock()
	if c, hit := g.cache[key]; hit && time.Since(c.at) < geocodeCacheTTL {
		g.mu.Unlock()
		return c.lat, c.lng, c.ok, nil
	}
	g.mu.Unlock()

	q := url.Values{}
	q.Set("key", g.token)
	q.Set("q", address)
	q.Set("format", "json")
	q.Set("limit", "1")
	q.Set("countrycodes", strings.ToLower(CountryOf(ctx)))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.base+"?"+q.Encode(), nil)
	if err != nil {
		return 0, 0, false, err
	}
	res, err := g.http.Do(req)
	if err != nil {
		return 0, 0, false, fmt.Errorf("locationiq: %w", err)
	}
	defer res.Body.Close()

	// LocationIQ answers 404 "Unable to geocode" for an address it can't
	// find: a real answer, cached like any other.
	if res.StatusCode == http.StatusNotFound {
		g.remember(key, cachedPoint{})
		return 0, 0, false, nil
	}
	if res.StatusCode != http.StatusOK {
		return 0, 0, false, fmt.Errorf("locationiq: HTTP %d", res.StatusCode)
	}
	var results []struct {
		Lat string `json:"lat"`
		Lon string `json:"lon"`
	}
	if err := json.NewDecoder(res.Body).Decode(&results); err != nil {
		return 0, 0, false, fmt.Errorf("locationiq: %w", err)
	}
	if len(results) == 0 {
		g.remember(key, cachedPoint{})
		return 0, 0, false, nil
	}
	lat, err1 := strconv.ParseFloat(results[0].Lat, 64)
	lng, err2 := strconv.ParseFloat(results[0].Lon, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false, fmt.Errorf("locationiq: bad coordinates")
	}
	g.remember(key, cachedPoint{lat: lat, lng: lng, ok: true})
	return lat, lng, true, nil
}

func (g *LocationIQGeocoder) remember(key string, p cachedPoint) {
	p.at = time.Now()
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.cache) >= geocodeCacheMax {
		g.cache = map[string]cachedPoint{} // crude, but bounded; refills from use
	}
	g.cache[key] = p
}

// LocateAddress tries the full address first and falls back to the ZIP alone,
// so a typo in the street still prices by the right neighbourhood.
//
// An "address" with no house number — the ZIP area the address search offers,
// such as "10002, Manhattan, New York County, New York, USA" — is located by
// its ZIP instead: geocoded as text it lands on the centre of the borough or
// city it names, the same point for every ZIP in it, which put every customer
// at one spot and made distances (and radius fees) wrong.
func LocateAddress(ctx context.Context, g AddressGeocoder, street, city, state, postcode string) (lat, lng float64, ok bool) {
	if g == nil {
		return 0, 0, false
	}
	if pc := strings.TrimSpace(postcode); pc != "" && !hasHouseNumber(street, pc) {
		if lat, lng, ok, err := g.GeocodeAddress(ctx, inCountry(ctx, pc)); err == nil && ok {
			return lat, lng, true
		}
	}
	var parts []string
	for _, p := range []string{street, city, strings.TrimSpace(state + " " + postcode)} {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) > 1 {
		if lat, lng, ok, err := g.GeocodeAddress(ctx, inCountry(ctx, strings.Join(parts, ", "))); err == nil && ok {
			return lat, lng, true
		}
	}
	if pc := strings.TrimSpace(postcode); pc != "" {
		if lat, lng, ok, err := g.GeocodeAddress(ctx, inCountry(ctx, pc)); err == nil && ok {
			return lat, lng, true
		}
	}
	return 0, 0, false
}

// hasHouseNumber reports whether a street line names a building: some digit
// other than the ZIP itself ("350 5th Ave", "1/23 Main St"). An area name or
// the ZIP alone has none.
func hasHouseNumber(street, postcode string) bool {
	s := strings.ReplaceAll(street, postcode, "")
	return strings.ContainsAny(s, "0123456789")
}

// DistanceKm returns the great-circle distance between two points in km.
func DistanceKm(lat1, lng1, lat2, lng2 float64) float64 {
	return DistanceMiles(lat1, lng1, lat2, lng2) * 1.609344
}
