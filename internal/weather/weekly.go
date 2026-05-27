package weather

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type NightSummary struct {
	Date          time.Time
	AvgCloudCover float64
	AvgCloudLow   float64
	AvgCloudMid   float64
	AvgCloudHigh  float64
	AvgWindSpeed  float64
	MaxPrecipProb float64
	AvgHumidity   float64
}

// FetchWeeklyNights fetches 7 days of hourly data and returns a nightly summary for each.
func FetchWeeklyNights(lat, lon float64, tz string) ([]NightSummary, error) {
	url := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%.2f&longitude=%.2f"+
			"&hourly=cloud_cover,cloud_cover_low,cloud_cover_mid,cloud_cover_high,"+
			"wind_speed_10m,precipitation_probability,relative_humidity_2m"+
			"&forecast_days=8&timezone=%s",
		lat, lon, tz,
	)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("open-meteo weekly request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("open-meteo returned %d: %s", resp.StatusCode, string(body))
	}

	var data OpenMeteoResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("open-meteo decode failed: %w", err)
	}

	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone: %w", err)
	}

	return extractWeeklyNights(data, loc)
}

func extractWeeklyNights(data OpenMeteoResponse, loc *time.Location) ([]NightSummary, error) {
	now := time.Now().In(loc)

	var summaries []NightSummary

	// For each of the next 7 nights, extract 9PM-4AM data
	for d := 0; d < 7; d++ {
		nightDate := time.Date(now.Year(), now.Month(), now.Day()+d, 0, 0, 0, 0, loc)
		nightStart := time.Date(now.Year(), now.Month(), now.Day()+d, 21, 0, 0, 0, loc)
		nightEnd := nightStart.Add(7 * time.Hour)

		var cloudSum, cloudLowSum, cloudMidSum, cloudHighSum, windSum, humSum float64
		var maxPrecip float64
		var count float64

		for i, ts := range data.Hourly.Time {
			t, err := time.ParseInLocation("2006-01-02T15:04", ts, loc)
			if err != nil {
				continue
			}
			if (t.Equal(nightStart) || t.After(nightStart)) && t.Before(nightEnd) {
				cloudSum += data.Hourly.CloudCover[i]
				cloudLowSum += data.Hourly.CloudCoverLow[i]
				cloudMidSum += data.Hourly.CloudCoverMid[i]
				cloudHighSum += data.Hourly.CloudCoverHigh[i]
				windSum += data.Hourly.WindSpeed[i]
				humSum += data.Hourly.RelativeHumidity[i]
				if data.Hourly.PrecipitationProbability[i] > maxPrecip {
					maxPrecip = data.Hourly.PrecipitationProbability[i]
				}
				count++
			}
		}

		if count > 0 {
			summaries = append(summaries, NightSummary{
				Date:          nightDate,
				AvgCloudCover: cloudSum / count,
				AvgCloudLow:   cloudLowSum / count,
				AvgCloudMid:   cloudMidSum / count,
				AvgCloudHigh:  cloudHighSum / count,
				AvgWindSpeed:  windSum / count,
				MaxPrecipProb: maxPrecip,
				AvgHumidity:   humSum / count,
			})
		}
	}

	return summaries, nil
}

// QuickScore produces a simplified 0-10 score from a NightSummary.
// Without pressure-level data for weekly forecasts, high clouds are penalized
// at 0.7 opacity (assume moderate thickness when unknown -- better to warn
// than to give a false positive that wastes an evening).
func QuickScore(n NightSummary) float64 {
	lowScore := 10 * (1 - n.AvgCloudLow/100)
	midScore := 10 * (1 - n.AvgCloudMid/100)
	highScore := 10 * (1 - n.AvgCloudHigh/100*0.7)
	cloudScore := lowScore*0.45 + midScore*0.30 + highScore*0.25

	var windScore float64
	switch {
	case n.AvgWindSpeed < 10:
		windScore = 10
	case n.AvgWindSpeed < 20:
		windScore = 10 - (n.AvgWindSpeed-10)*0.4
	case n.AvgWindSpeed < 30:
		windScore = 6 - (n.AvgWindSpeed-20)*0.3
	default:
		windScore = 0
	}

	var precipScore float64
	if n.MaxPrecipProb <= 0 {
		precipScore = 10
	} else if n.MaxPrecipProb >= 50 {
		precipScore = 0
	} else {
		precipScore = 10 * (1 - n.MaxPrecipProb/50)
	}

	// Humidity as a rough transparency proxy for multi-day forecasts
	var humidityScore float64
	switch {
	case n.AvgHumidity < 50:
		humidityScore = 10
	case n.AvgHumidity < 65:
		humidityScore = 7
	case n.AvgHumidity < 80:
		humidityScore = 4
	default:
		humidityScore = 1
	}

	return cloudScore*0.50 + humidityScore*0.20 + windScore*0.10 + precipScore*0.20
}
