package astro

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type VisiblePlanetsResponse []PlanetData

type PlanetData struct {
	Name           string  `json:"name"`
	Altitude       float64 `json:"altitude"`
	Azimuth        float64 `json:"azimuth"`
	Magnitude      float64 `json:"magnitude"`
	Constellation  string  `json:"constellation"`
	AboveHorizon   bool    `json:"aboveHorizon"`
	NakedEyeObject bool    `json:"nakedEyeObject"`
}

type PlanetInfo struct {
	Name          string
	Magnitude     float64
	Constellation string
}

// FetchVisiblePlanets fetches planets visible at ~10PM local time tonight.
func FetchVisiblePlanets(lat, lon, elevation float64, tz string) ([]PlanetInfo, error) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone: %w", err)
	}

	// Check at 10PM tonight
	now := time.Now().In(loc)
	tonight := time.Date(now.Year(), now.Month(), now.Day(), 22, 0, 0, 0, loc)
	if now.After(tonight) {
		tonight = tonight.Add(24 * time.Hour)
	}

	url := fmt.Sprintf(
		"https://api.visibleplanets.dev/v3?latitude=%.4f&longitude=%.4f&elevation=%.0f&aboveHorizon=true&time=%s",
		lat, lon, elevation, tonight.UTC().Format(time.RFC3339),
	)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}

	var data VisiblePlanetsResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, nil
	}

	var planets []PlanetInfo
	for _, p := range data {
		// Skip Sun and Moon, only report planets
		if p.Name == "Sun" || p.Name == "Moon" {
			continue
		}
		if p.AboveHorizon && p.NakedEyeObject {
			planets = append(planets, PlanetInfo{
				Name:          p.Name,
				Magnitude:     p.Magnitude,
				Constellation: p.Constellation,
			})
		}
	}

	return planets, nil
}
