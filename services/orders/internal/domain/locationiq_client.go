package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"bakehouse/pkg/geo"
	"bakehouse/services/orders/ordersapi"
)

type locationIQClient struct {
	accessToken string
	http        *http.Client
}

func newLocationIQClient(accessToken string) *locationIQClient {
	return &locationIQClient{
		accessToken: accessToken,
		http:        &http.Client{Timeout: 8 * time.Second},
	}
}

func (c *locationIQClient) Configured() bool { return c != nil && c.accessToken != "" }

type locationIQResult struct {
	PlaceID     string `json:"place_id"`
	Lat         string `json:"lat"`
	Lon         string `json:"lon"`
	DisplayName string `json:"display_name"`
	Address     struct {
		HouseNumber string `json:"house_number"`
		Road        string `json:"road"`
		City        string `json:"city"`
		Town        string `json:"town"`
		Village     string `json:"village"`
		State       string `json:"state"`
		Postcode    string `json:"postcode"`
		Country     string `json:"country"`
	} `json:"address"`
}

func (c *locationIQClient) Search(ctx context.Context, query string) ([]ordersapi.AddressResult, error) {
	// Only the business's own country, from LocationIQ's EU region for any
	// country but the US (UK addresses stay in Europe).
	country := geo.CountryOf(ctx)
	host := "eu1"
	if country == "US" {
		host = "us1"
	}
	u := fmt.Sprintf(
		"https://%s.locationiq.com/v1/search?key=%s&q=%s&format=json&addressdetails=1&limit=5&countrycodes=%s",
		host, url.QueryEscape(c.accessToken), url.QueryEscape(query), strings.ToLower(country),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return []ordersapi.AddressResult{}, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("locationiq: status %d: %s", resp.StatusCode, string(body))
	}

	var raw []locationIQResult
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	out := make([]ordersapi.AddressResult, 0, len(raw))
	for _, r := range raw {
		city := r.Address.City
		if city == "" {
			city = r.Address.Town
		}
		if city == "" {
			city = r.Address.Village
		}
		out = append(out, ordersapi.AddressResult{
			PlaceID:     r.PlaceID,
			DisplayName: r.DisplayName,
			Street:      strings.TrimSpace(r.Address.HouseNumber + " " + r.Address.Road),
			City:        city,
			State:       r.Address.State,
			Postcode:    r.Address.Postcode,
			Country:     r.Address.Country,
			Latitude:    r.Lat,
			Longitude:   r.Lon,
		})
	}
	return out, nil
}
