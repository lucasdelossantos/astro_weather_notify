package weather

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type SevenTimerResponse struct {
	Product    string           `json:"product"`
	Init       string           `json:"init"`
	Dataseries []SevenTimerData `json:"dataseries"`
}

type SevenTimerData struct {
	Timepoint    int              `json:"timepoint"`
	CloudCover   int              `json:"cloudcover"`
	Seeing       int              `json:"seeing"`
	Transparency int              `json:"transparency"`
	LiftedIndex  int              `json:"lifted_index"`
	RH2m         int              `json:"rh2m"`
	Wind10m      SevenTimerWind   `json:"wind10m"`
	Temp2m       int              `json:"temp2m"`
	PrecType     string           `json:"prec_type"`
}

type SevenTimerWind struct {
	Direction string `json:"direction"`
	Speed     int    `json:"speed"`
}

// AstroConditions holds seeing and transparency for the night.
type AstroConditions struct {
	Seeing       int // 1-8, lower is better
	Transparency int // 1-8, lower is better
	CloudCover   int // 1-9, lower is better
	Available    bool
}

// FetchAstroConditions fetches 7Timer astro forecast and extracts tonight's conditions.
func FetchAstroConditions(lat, lon float64) (*AstroConditions, error) {
	url := fmt.Sprintf(
		"https://www.7timer.info/bin/astro.php?lon=%.2f&lat=%.2f&ac=0&unit=metric&output=json&tzshift=0",
		lon, lat,
	)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return &AstroConditions{Available: false}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &AstroConditions{Available: false}, nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &AstroConditions{Available: false}, nil
	}

	var data SevenTimerResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return &AstroConditions{Available: false}, nil
	}

	return extractNightAstro(data), nil
}

// extractNightAstro finds the data points closest to tonight's prime hours.
// 7Timer uses timepoints as hours from init time. We look for points
// that fall in the nighttime window and average them.
func extractNightAstro(data SevenTimerResponse) *AstroConditions {
	if len(data.Dataseries) == 0 {
		return &AstroConditions{Available: false}
	}

	// Parse init time (format: "2026052100" = YYYYMMDDHH in UTC)
	initTime, err := time.Parse("2006010215", data.Init)
	if err != nil {
		return &AstroConditions{Available: false}
	}

	now := time.Now().UTC()
	// Tonight window in UTC (approximate: 1AM-8AM UTC = 9PM-4AM ET)
	tonightStart := time.Date(now.Year(), now.Month(), now.Day(), 1, 0, 0, 0, time.UTC)
	if now.Hour() >= 12 {
		// If it's afternoon, tonight means the upcoming night
		tonightStart = tonightStart.Add(24 * time.Hour)
	}
	tonightEnd := tonightStart.Add(7 * time.Hour)

	var seeingSum, transSum, cloudSum, count int
	for _, dp := range data.Dataseries {
		dpTime := initTime.Add(time.Duration(dp.Timepoint) * time.Hour)
		if (dpTime.Equal(tonightStart) || dpTime.After(tonightStart)) && dpTime.Before(tonightEnd) {
			seeingSum += dp.Seeing
			transSum += dp.Transparency
			cloudSum += dp.CloudCover
			count++
		}
	}

	if count == 0 {
		// Fall back to first nighttime-ish data point
		for _, dp := range data.Dataseries {
			dpTime := initTime.Add(time.Duration(dp.Timepoint) * time.Hour)
			if dpTime.Hour() >= 22 || dpTime.Hour() <= 4 {
				return &AstroConditions{
					Seeing:       dp.Seeing,
					Transparency: dp.Transparency,
					CloudCover:   dp.CloudCover,
					Available:    true,
				}
			}
		}
		return &AstroConditions{Available: false}
	}

	return &AstroConditions{
		Seeing:       seeingSum / count,
		Transparency: transSum / count,
		CloudCover:   cloudSum / count,
		Available:    true,
	}
}
