package astro

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type AuroraForecast struct {
	CurrentKp  float64
	MaxKp24h   float64
	Visible    bool   // whether aurora is likely visible at this latitude
	AlertLevel string // none, possible, likely, strong
}

// FetchAuroraForecast gets current and predicted Kp index from NOAA SWPC.
// At latitude 42.4N, aurora becomes visible around Kp 6+.
func FetchAuroraForecast() (*AuroraForecast, error) {
	// NOAA planetary K-index (current and 3-day forecast)
	url := "https://services.swpc.noaa.gov/products/noaa-planetary-k-index-forecast.json"

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("noaa kp request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("noaa kp returned %d", resp.StatusCode)
	}

	type kpEntry struct {
		TimeTag  string  `json:"time_tag"`
		Kp       float64 `json:"kp"`
		Observed string  `json:"observed"`
	}

	var entries []kpEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("noaa kp decode failed: %w", err)
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("noaa kp: no data")
	}

	forecast := &AuroraForecast{}
	now := time.Now().UTC()

	for _, entry := range entries {
		t, err := time.Parse("2006-01-02T15:04:05", entry.TimeTag)
		if err != nil {
			continue
		}

		// Find current (closest past entry)
		if t.Before(now) || t.Equal(now) {
			forecast.CurrentKp = entry.Kp
		}

		// Track max in next 24h
		if t.After(now) && t.Before(now.Add(24*time.Hour)) {
			if entry.Kp > forecast.MaxKp24h {
				forecast.MaxKp24h = entry.Kp
			}
		}
	}

	// If no future data found, use current as max
	if forecast.MaxKp24h == 0 {
		forecast.MaxKp24h = forecast.CurrentKp
	}

	// At 42.4N latitude, aurora visibility thresholds
	maxKp := forecast.MaxKp24h
	switch {
	case maxKp >= 8:
		forecast.AlertLevel = "strong"
		forecast.Visible = true
	case maxKp >= 6:
		forecast.AlertLevel = "likely"
		forecast.Visible = true
	case maxKp >= 5:
		forecast.AlertLevel = "possible"
		forecast.Visible = true
	default:
		forecast.AlertLevel = "none"
		forecast.Visible = false
	}

	return forecast, nil
}

