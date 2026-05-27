package geo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Location struct {
	Name      string
	Latitude  float64
	Longitude float64
	Elevation float64
	Timezone  string
}

type geocodeResponse struct {
	Results []geocodeResult `json:"results"`
}

type geocodeResult struct {
	Name      string  `json:"name"`
	Admin1    string  `json:"admin1"`
	Country   string  `json:"country"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Elevation float64 `json:"elevation"`
	Timezone  string  `json:"timezone"`
}

// Lookup resolves a place name to coordinates using Open-Meteo's geocoding API.
func Lookup(query string) (*Location, error) {
	// Normalize: strip state/country suffixes after comma -- the API works best with city name only
	query = strings.TrimSpace(query)
	parts := strings.SplitN(query, ",", 2)
	cityName := strings.TrimSpace(parts[0])

	u := fmt.Sprintf(
		"https://geocoding-api.open-meteo.com/v1/search?name=%s&count=1&language=en&format=json",
		url.QueryEscape(cityName),
	)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(u)
	if err != nil {
		return nil, fmt.Errorf("geocoding request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("geocoding returned %d", resp.StatusCode)
	}

	var data geocodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("geocoding decode failed: %w", err)
	}

	if len(data.Results) == 0 {
		return nil, fmt.Errorf("no results found for '%s'", query)
	}

	r := data.Results[0]
	name := r.Name
	if r.Admin1 != "" {
		name += ", " + r.Admin1
	}

	return &Location{
		Name:      name,
		Latitude:  r.Latitude,
		Longitude: r.Longitude,
		Elevation: r.Elevation,
		Timezone:  r.Timezone,
	}, nil
}
