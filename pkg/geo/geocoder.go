package geo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Geocoder resolves a postcode, in the request's country, to coordinates. Nearest-branch ranking
// (SF-03, SF-05) degrades gracefully without one: NoopGeocoder always
// reports ok=false, and callers fall back to an unranked branch list rather
// than failing the request — a missing Google Maps API key should never
// take checkout down.
type Geocoder interface {
	Geocode(ctx context.Context, postcode string) (lat, lng float64, ok bool, err error)
}

// NoopGeocoder is used when GOOGLE_MAPS_API_KEY is not configured.
type NoopGeocoder struct{}

func (NoopGeocoder) Geocode(ctx context.Context, postcode string) (float64, float64, bool, error) {
	return 0, 0, false, nil
}

// GoogleMapsGeocoder calls the Google Maps Geocoding API.
type GoogleMapsGeocoder struct {
	apiKey     string
	httpClient *http.Client
}

func NewGoogleMapsGeocoder(apiKey string) *GoogleMapsGeocoder {
	return &GoogleMapsGeocoder{apiKey: apiKey, httpClient: &http.Client{Timeout: 5 * time.Second}}
}

type geocodeResponse struct {
	Status  string `json:"status"`
	Results []struct {
		Geometry struct {
			Location struct {
				Lat float64 `json:"lat"`
				Lng float64 `json:"lng"`
			} `json:"location"`
		} `json:"geometry"`
	} `json:"results"`
}

func (g *GoogleMapsGeocoder) Geocode(ctx context.Context, postcode string) (float64, float64, bool, error) {
	q := url.Values{}
	q.Set("address", inCountry(ctx, postcode))
	q.Set("components", "country:"+CountryOf(ctx))
	q.Set("key", g.apiKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://maps.googleapis.com/maps/api/geocode/json?"+q.Encode(), nil)
	if err != nil {
		return 0, 0, false, fmt.Errorf("build geocode request: %w", err)
	}

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return 0, 0, false, fmt.Errorf("geocode request failed: %w", err)
	}
	defer resp.Body.Close()

	var out geocodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, 0, false, fmt.Errorf("decode geocode response: %w", err)
	}
	if out.Status != "OK" || len(out.Results) == 0 {
		return 0, 0, false, nil
	}
	loc := out.Results[0].Geometry.Location
	return loc.Lat, loc.Lng, true, nil
}
