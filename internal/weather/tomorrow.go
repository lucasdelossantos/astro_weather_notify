package weather

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type tomorrowResponse struct {
	Timelines struct {
		Hourly []struct {
			Time   string         `json:"time"`
			Values tomorrowValues `json:"values"`
		} `json:"hourly"`
	} `json:"timelines"`
}

type tomorrowValues struct {
	CloudCover               float64 `json:"cloudCover"`
	Humidity                 float64 `json:"humidity"`
	DewPoint                 float64 `json:"dewPoint"`
	Temperature              float64 `json:"temperature"`
	WindSpeed                float64 `json:"windSpeed"`
	WindGust                 float64 `json:"windGust"`
	Visibility               float64 `json:"visibility"`
	PrecipitationProbability float64 `json:"precipitationProbability"`
}

// FetchNighttimeWeatherTomorrow uses Tomorrow.io as a fallback weather source.
// Returns total cloud cover only (no low/mid/high split).
func FetchNighttimeWeatherTomorrow(lat, lon float64, tz, apiKey string) (*NighttimeWeather, error) {
	url := fmt.Sprintf(
		"https://api.tomorrow.io/v4/weather/forecast?location=%.4f,%.4f"+
			"&apikey=%s&timesteps=1h&units=metric",
		lat, lon, apiKey,
	)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("tomorrow.io request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("tomorrow.io returned %d: %s", resp.StatusCode, string(body))
	}

	var data tomorrowResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("tomorrow.io decode failed: %w", err)
	}

	return extractNighttimeTomorrow(data, tz)
}

func extractNighttimeTomorrow(data tomorrowResponse, tz string) (*NighttimeWeather, error) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone %s: %w", tz, err)
	}

	now := time.Now().In(loc)
	tonightStart := time.Date(now.Year(), now.Month(), now.Day(), 21, 0, 0, 0, loc)
	tonightEnd := tonightStart.Add(7 * time.Hour)

	result := &NighttimeWeather{}
	var totalCloud, totalHumidity, totalDew, totalTemp, totalWind, totalVis float64
	var count float64

	for _, h := range data.Timelines.Hourly {
		t, err := time.Parse(time.RFC3339, h.Time)
		if err != nil {
			continue
		}
		tLocal := t.In(loc)
		if (tLocal.Equal(tonightStart) || tLocal.After(tonightStart)) && tLocal.Before(tonightEnd) {
			totalCloud += h.Values.CloudCover
			totalHumidity += h.Values.Humidity
			totalDew += h.Values.DewPoint
			totalTemp += h.Values.Temperature
			totalWind += h.Values.WindSpeed * 3.6 // m/s to km/h
			totalVis += h.Values.Visibility * 1000 // km to m

			if h.Values.WindGust*3.6 > result.MaxWindGusts {
				result.MaxWindGusts = h.Values.WindGust * 3.6
			}
			if h.Values.PrecipitationProbability > result.MaxPrecipProb {
				result.MaxPrecipProb = h.Values.PrecipitationProbability
			}

			result.HourlyDetail = append(result.HourlyDetail, HourDetail{
				Time:       tLocal,
				CloudCover: h.Values.CloudCover,
				WindSpeed:  h.Values.WindSpeed * 3.6,
				Visibility: h.Values.Visibility * 1000,
			})
			count++
		}
	}

	if count == 0 {
		return nil, fmt.Errorf("no nighttime hours found in tomorrow.io data")
	}

	result.AvgCloudCover = totalCloud / count
	// No layer split available -- distribute total evenly as a rough approximation
	result.AvgCloudCoverLow = totalCloud / count * 0.4
	result.AvgCloudCoverMid = totalCloud / count * 0.3
	result.AvgCloudCoverHigh = totalCloud / count * 0.3
	result.AvgHumidity = totalHumidity / count
	result.AvgDewPoint = totalDew / count
	result.AvgTemperature = totalTemp / count
	result.AvgWindSpeed = totalWind / count
	result.AvgVisibility = totalVis / count

	return result, nil
}
