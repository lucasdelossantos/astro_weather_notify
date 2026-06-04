package weather

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

var meteoClient = &http.Client{Timeout: 30 * time.Second}

var (
	meteoMu       sync.Mutex
	meteoLastCall time.Time
)

const meteoMinInterval = 2 * time.Second

// meteoBackoff tracks when to skip Open-Meteo entirely after repeated 429s.
var meteoBackoffUntil time.Time

// meteoGet serializes requests, enforces minimum spacing, and retries on 429.
// After a failure, backs off for 5 minutes to avoid slow fallback paths.
func meteoGet(url string) (*http.Response, error) {
	meteoMu.Lock()
	defer meteoMu.Unlock()

	if time.Now().Before(meteoBackoffUntil) {
		return nil, fmt.Errorf("open-meteo backed off until %s", meteoBackoffUntil.Format(time.Kitchen))
	}

	if elapsed := time.Since(meteoLastCall); elapsed < meteoMinInterval {
		time.Sleep(meteoMinInterval - elapsed)
	}

	meteoLastCall = time.Now()
	resp, err := meteoClient.Get(url)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusTooManyRequests {
		return resp, nil
	}
	resp.Body.Close()

	// Single 429 triggers a 5-minute backoff so subsequent calls fail fast
	meteoBackoffUntil = time.Now().Add(5 * time.Minute)
	return nil, fmt.Errorf("rate limited, backing off for 5 minutes")
}

type OpenMeteoResponse struct {
	Hourly OpenMeteoHourly `json:"hourly"`
}

type OpenMeteoHourly struct {
	Time                     []string  `json:"time"`
	CloudCover               []float64 `json:"cloud_cover"`
	CloudCoverLow            []float64 `json:"cloud_cover_low"`
	CloudCoverMid            []float64 `json:"cloud_cover_mid"`
	CloudCoverHigh           []float64 `json:"cloud_cover_high"`
	Visibility               []float64 `json:"visibility"`
	RelativeHumidity         []float64 `json:"relative_humidity_2m"`
	DewPoint                 []float64 `json:"dew_point_2m"`
	Temperature              []float64 `json:"temperature_2m"`
	WindSpeed                []float64 `json:"wind_speed_10m"`
	WindGusts                []float64 `json:"wind_gusts_10m"`
	PrecipitationProbability []float64 `json:"precipitation_probability"`
	IsDay                    []int     `json:"is_day"`
}

// NighttimeWeather holds aggregated weather conditions for the nighttime window.
type NighttimeWeather struct {
	AvgCloudCover     float64
	AvgCloudCoverLow  float64
	AvgCloudCoverMid  float64
	AvgCloudCoverHigh float64
	AvgHumidity       float64
	AvgDewPoint       float64
	AvgTemperature    float64
	AvgWindSpeed      float64
	MaxWindGusts      float64
	AvgVisibility     float64
	MaxPrecipProb     float64
	HourlyDetail      []HourDetail

	// Pressure-level cloud profile for thickness estimation
	CloudCover300hPa float64
	CloudCover250hPa float64
	CloudCover200hPa float64
	CloudCover150hPa float64
	RH300hPa         float64
	RH250hPa         float64
	RH200hPa         float64
	JetStreamSpeed   float64 // wind at 250 hPa in m/s

	// ECMWF integrated water vapor (transparency proxy)
	AvgIWV float64 // kg/m^2

	// CAMS aerosol optical depth
	AvgAOD float64
}

type HourDetail struct {
	Time       time.Time
	CloudCover float64
	WindSpeed  float64
	Visibility float64
}

// FetchNighttimeWeather fetches forecast data and extracts tonight's nighttime hours (9PM-4AM).
func FetchNighttimeWeather(lat, lon float64, tz string) (*NighttimeWeather, error) {
	url := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%.2f&longitude=%.2f"+
			"&hourly=cloud_cover,cloud_cover_low,cloud_cover_mid,cloud_cover_high,"+
			"visibility,relative_humidity_2m,dew_point_2m,temperature_2m,wind_speed_10m,wind_gusts_10m,"+
			"precipitation_probability,is_day&forecast_days=2&timezone=%s",
		lat, lon, tz,
	)

	resp, err := meteoGet(url)
	if err != nil {
		return nil, fmt.Errorf("open-meteo request failed: %w", err)
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

	return extractNighttime(data, tz)
}

// extractNighttime pulls out hours between 9PM today and 4AM tomorrow.
func extractNighttime(data OpenMeteoResponse, tz string) (*NighttimeWeather, error) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone %s: %w", tz, err)
	}

	now := time.Now().In(loc)
	// Tonight's window: 9PM today to 4AM tomorrow
	tonightStart := time.Date(now.Year(), now.Month(), now.Day(), 21, 0, 0, 0, loc)
	tonightEnd := tonightStart.Add(7 * time.Hour) // 4AM next day

	var nightIndices []int
	for i, ts := range data.Hourly.Time {
		t, err := time.ParseInLocation("2006-01-02T15:04", ts, loc)
		if err != nil {
			continue
		}
		if (t.Equal(tonightStart) || t.After(tonightStart)) && t.Before(tonightEnd) {
			nightIndices = append(nightIndices, i)
		}
	}

	if len(nightIndices) == 0 {
		return nil, fmt.Errorf("no nighttime hours found in forecast data")
	}

	result := &NighttimeWeather{}
	var totalCloud, totalCloudLow, totalCloudMid, totalCloudHigh, totalHumidity, totalDew, totalTemp, totalWind, totalVis float64

	for _, i := range nightIndices {
		totalCloud += data.Hourly.CloudCover[i]
		totalCloudLow += data.Hourly.CloudCoverLow[i]
		totalCloudMid += data.Hourly.CloudCoverMid[i]
		totalCloudHigh += data.Hourly.CloudCoverHigh[i]
		totalHumidity += data.Hourly.RelativeHumidity[i]
		totalDew += data.Hourly.DewPoint[i]
		totalTemp += data.Hourly.Temperature[i]
		totalWind += data.Hourly.WindSpeed[i]
		totalVis += data.Hourly.Visibility[i]

		if data.Hourly.WindGusts[i] > result.MaxWindGusts {
			result.MaxWindGusts = data.Hourly.WindGusts[i]
		}
		if data.Hourly.PrecipitationProbability[i] > result.MaxPrecipProb {
			result.MaxPrecipProb = data.Hourly.PrecipitationProbability[i]
		}

		t, _ := time.ParseInLocation("2006-01-02T15:04", data.Hourly.Time[i], loc)
		result.HourlyDetail = append(result.HourlyDetail, HourDetail{
			Time:       t,
			CloudCover: data.Hourly.CloudCover[i],
			WindSpeed:  data.Hourly.WindSpeed[i],
			Visibility: data.Hourly.Visibility[i],
		})
	}

	n := float64(len(nightIndices))
	result.AvgCloudCover = totalCloud / n
	result.AvgCloudCoverLow = totalCloudLow / n
	result.AvgCloudCoverMid = totalCloudMid / n
	result.AvgCloudCoverHigh = totalCloudHigh / n
	result.AvgHumidity = totalHumidity / n
	result.AvgDewPoint = totalDew / n
	result.AvgTemperature = totalTemp / n
	result.AvgWindSpeed = totalWind / n
	result.AvgVisibility = totalVis / n

	return result, nil
}

// Pressure-level response for cloud thickness estimation.
type PressureLevelResponse struct {
	Hourly PressureLevelHourly `json:"hourly"`
}

type PressureLevelHourly struct {
	Time            []string  `json:"time"`
	CloudCover300   []float64 `json:"cloud_cover_300hPa"`
	CloudCover250   []float64 `json:"cloud_cover_250hPa"`
	CloudCover200   []float64 `json:"cloud_cover_200hPa"`
	CloudCover150   []float64 `json:"cloud_cover_150hPa"`
	RH300           []float64 `json:"relative_humidity_300hPa"`
	RH250           []float64 `json:"relative_humidity_250hPa"`
	RH200           []float64 `json:"relative_humidity_200hPa"`
	WindSpeed250    []float64 `json:"wind_speed_250hPa"`
}

// FetchPressureLevelData fetches upper-atmosphere cloud and humidity profiles
// to estimate high cloud thickness and detect jet stream presence.
func FetchPressureLevelData(lat, lon float64, tz string) (*PressureLevelResponse, error) {
	url := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%.2f&longitude=%.2f"+
			"&hourly=cloud_cover_300hPa,cloud_cover_250hPa,cloud_cover_200hPa,cloud_cover_150hPa,"+
			"relative_humidity_300hPa,relative_humidity_250hPa,relative_humidity_200hPa,"+
			"wind_speed_250hPa"+
			"&models=gfs_seamless&forecast_days=2&timezone=%s",
		lat, lon, tz,
	)

	resp, err := meteoGet(url)
	if err != nil {
		return nil, fmt.Errorf("pressure-level request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("pressure-level returned %d: %s", resp.StatusCode, string(body))
	}

	var data PressureLevelResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("pressure-level decode failed: %w", err)
	}

	return &data, nil
}

// ExtractNighttimePressureLevel extracts pressure-level averages for the 9PM-4AM window
// and populates the corresponding fields on NighttimeWeather.
func ExtractNighttimePressureLevel(wx *NighttimeWeather, data *PressureLevelResponse, tz string) error {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return fmt.Errorf("invalid timezone %s: %w", tz, err)
	}

	now := time.Now().In(loc)
	tonightStart := time.Date(now.Year(), now.Month(), now.Day(), 21, 0, 0, 0, loc)
	tonightEnd := tonightStart.Add(7 * time.Hour)

	var cc300, cc250, cc200, cc150, rh300, rh250, rh200, ws250 float64
	var count float64

	for i, ts := range data.Hourly.Time {
		t, err := time.ParseInLocation("2006-01-02T15:04", ts, loc)
		if err != nil {
			continue
		}
		if (t.Equal(tonightStart) || t.After(tonightStart)) && t.Before(tonightEnd) {
			cc300 += data.Hourly.CloudCover300[i]
			cc250 += data.Hourly.CloudCover250[i]
			cc200 += data.Hourly.CloudCover200[i]
			cc150 += data.Hourly.CloudCover150[i]
			rh300 += data.Hourly.RH300[i]
			rh250 += data.Hourly.RH250[i]
			rh200 += data.Hourly.RH200[i]
			ws250 += data.Hourly.WindSpeed250[i]
			count++
		}
	}

	if count == 0 {
		return fmt.Errorf("no nighttime pressure-level data found")
	}

	wx.CloudCover300hPa = cc300 / count
	wx.CloudCover250hPa = cc250 / count
	wx.CloudCover200hPa = cc200 / count
	wx.CloudCover150hPa = cc150 / count
	wx.RH300hPa = rh300 / count
	wx.RH250hPa = rh250 / count
	wx.RH200hPa = rh200 / count
	wx.JetStreamSpeed = ws250 / count

	return nil
}

// ECMWF response for integrated water vapor.
type ECMWFResponse struct {
	Hourly ECMWFHourly `json:"hourly"`
}

type ECMWFHourly struct {
	Time []string  `json:"time"`
	IWV  []float64 `json:"total_column_integrated_water_vapour"`
}

// FetchECMWFData fetches total column integrated water vapor from the ECMWF model.
func FetchECMWFData(lat, lon float64, tz string) (*ECMWFResponse, error) {
	url := fmt.Sprintf(
		"https://api.open-meteo.com/v1/ecmwf?latitude=%.2f&longitude=%.2f"+
			"&hourly=total_column_integrated_water_vapour"+
			"&forecast_days=2&timezone=%s",
		lat, lon, tz,
	)

	resp, err := meteoGet(url)
	if err != nil {
		return nil, fmt.Errorf("ecmwf request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ecmwf returned %d: %s", resp.StatusCode, string(body))
	}

	var data ECMWFResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("ecmwf decode failed: %w", err)
	}

	return &data, nil
}

// ExtractNighttimeIWV extracts the average integrated water vapor for the nighttime window.
func ExtractNighttimeIWV(wx *NighttimeWeather, data *ECMWFResponse, tz string) error {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return fmt.Errorf("invalid timezone %s: %w", tz, err)
	}

	now := time.Now().In(loc)
	tonightStart := time.Date(now.Year(), now.Month(), now.Day(), 21, 0, 0, 0, loc)
	tonightEnd := tonightStart.Add(7 * time.Hour)

	var totalIWV float64
	var count float64

	for i, ts := range data.Hourly.Time {
		t, err := time.ParseInLocation("2006-01-02T15:04", ts, loc)
		if err != nil {
			continue
		}
		if (t.Equal(tonightStart) || t.After(tonightStart)) && t.Before(tonightEnd) {
			totalIWV += data.Hourly.IWV[i]
			count++
		}
	}

	if count == 0 {
		return fmt.Errorf("no nighttime ECMWF data found")
	}

	wx.AvgIWV = totalIWV / count
	return nil
}

// Air quality response for aerosol optical depth.
type AirQualityResponse struct {
	Hourly AirQualityHourly `json:"hourly"`
}

type AirQualityHourly struct {
	Time []string  `json:"time"`
	AOD  []float64 `json:"aerosol_optical_depth"`
}

// FetchAirQuality fetches aerosol optical depth from the CAMS model via Open-Meteo.
func FetchAirQuality(lat, lon float64, tz string) (*AirQualityResponse, error) {
	url := fmt.Sprintf(
		"https://air-quality-api.open-meteo.com/v1/air-quality?latitude=%.2f&longitude=%.2f"+
			"&hourly=aerosol_optical_depth"+
			"&forecast_days=2&timezone=%s",
		lat, lon, tz,
	)

	resp, err := meteoGet(url)
	if err != nil {
		return nil, fmt.Errorf("air-quality request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("air-quality returned %d: %s", resp.StatusCode, string(body))
	}

	var data AirQualityResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("air-quality decode failed: %w", err)
	}

	return &data, nil
}

// ExtractNighttimeAOD extracts the average aerosol optical depth for the nighttime window.
func ExtractNighttimeAOD(wx *NighttimeWeather, data *AirQualityResponse, tz string) error {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return fmt.Errorf("invalid timezone %s: %w", tz, err)
	}

	now := time.Now().In(loc)
	tonightStart := time.Date(now.Year(), now.Month(), now.Day(), 21, 0, 0, 0, loc)
	tonightEnd := tonightStart.Add(7 * time.Hour)

	var totalAOD float64
	var count float64

	for i, ts := range data.Hourly.Time {
		t, err := time.ParseInLocation("2006-01-02T15:04", ts, loc)
		if err != nil {
			continue
		}
		if (t.Equal(tonightStart) || t.After(tonightStart)) && t.Before(tonightEnd) {
			totalAOD += data.Hourly.AOD[i]
			count++
		}
	}

	if count == 0 {
		return fmt.Errorf("no nighttime air quality data found")
	}

	wx.AvgAOD = totalAOD / count
	return nil
}
