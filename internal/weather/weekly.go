package weather

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
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
	MaxWindGusts  float64
	MaxPrecipProb float64
	AvgHumidity   float64
	AvgDewPoint   float64
	AvgTemp       float64
	AvgVisibility float64
	Weather       *NighttimeWeather
}

// FetchWeeklyNights fetches 8 days of hourly data and returns a nightly summary
// for each of the next 7 nights, with full NighttimeWeather structs for accurate scoring.
func FetchWeeklyNights(lat, lon float64, tz string) ([]NightSummary, error) {
	url := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%.2f&longitude=%.2f"+
			"&hourly=cloud_cover,cloud_cover_low,cloud_cover_mid,cloud_cover_high,"+
			"visibility,relative_humidity_2m,dew_point_2m,temperature_2m,"+
			"wind_speed_10m,wind_gusts_10m,precipitation_probability"+
			"&forecast_days=8&timezone=%s",
		lat, lon, tz,
	)

	resp, err := meteoGet(url)
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

	summaries := extractWeeklyNights(data, loc)

	// Fetch pressure-level data for all 8 days in one call
	pressureData, err := fetchWeeklyPressureLevel(lat, lon, tz)
	if err != nil {
		log.Printf("weekly pressure-level data unavailable: %v", err)
	}

	// Fetch IWV for all 8 days
	iwvData, err := fetchWeeklyIWV(lat, lon, tz)
	if err != nil {
		log.Printf("weekly IWV data unavailable: %v", err)
	}

	// Fetch AOD for all 8 days
	aodData, err := fetchWeeklyAOD(lat, lon, tz)
	if err != nil {
		log.Printf("weekly AOD data unavailable: %v", err)
	}

	// Populate pressure-level, IWV, and AOD data for each night
	for i := range summaries {
		if summaries[i].Weather == nil {
			continue
		}
		nightStart := time.Date(
			summaries[i].Date.Year(), summaries[i].Date.Month(), summaries[i].Date.Day(),
			21, 0, 0, 0, loc,
		)
		nightEnd := nightStart.Add(7 * time.Hour)

		if pressureData != nil {
			extractPressureForWindow(summaries[i].Weather, pressureData, loc, nightStart, nightEnd)
		}
		if iwvData != nil {
			extractIWVForWindow(summaries[i].Weather, iwvData, loc, nightStart, nightEnd)
		}
		if aodData != nil {
			extractAODForWindow(summaries[i].Weather, aodData, loc, nightStart, nightEnd)
		}
	}

	return summaries, nil
}

func extractWeeklyNights(data OpenMeteoResponse, loc *time.Location) []NightSummary {
	now := time.Now().In(loc)

	var summaries []NightSummary

	for d := 0; d < 7; d++ {
		nightDate := time.Date(now.Year(), now.Month(), now.Day()+d, 0, 0, 0, 0, loc)
		nightStart := time.Date(now.Year(), now.Month(), now.Day()+d, 21, 0, 0, 0, loc)
		nightEnd := nightStart.Add(7 * time.Hour)

		var cloudSum, cloudLowSum, cloudMidSum, cloudHighSum float64
		var windSum, gustMax, humSum, dewSum, tempSum, visSum float64
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
				dewSum += data.Hourly.DewPoint[i]
				tempSum += data.Hourly.Temperature[i]
				visSum += data.Hourly.Visibility[i]

				if data.Hourly.WindGusts[i] > gustMax {
					gustMax = data.Hourly.WindGusts[i]
				}
				if data.Hourly.PrecipitationProbability[i] > maxPrecip {
					maxPrecip = data.Hourly.PrecipitationProbability[i]
				}
				count++
			}
		}

		if count > 0 {
			wx := &NighttimeWeather{
				AvgCloudCover:     cloudSum / count,
				AvgCloudCoverLow:  cloudLowSum / count,
				AvgCloudCoverMid:  cloudMidSum / count,
				AvgCloudCoverHigh: cloudHighSum / count,
				AvgHumidity:       humSum / count,
				AvgDewPoint:       dewSum / count,
				AvgTemperature:    tempSum / count,
				AvgWindSpeed:      windSum / count,
				MaxWindGusts:      gustMax,
				AvgVisibility:     visSum / count,
				MaxPrecipProb:     maxPrecip,
			}

			summaries = append(summaries, NightSummary{
				Date:          nightDate,
				AvgCloudCover: wx.AvgCloudCover,
				AvgCloudLow:   wx.AvgCloudCoverLow,
				AvgCloudMid:   wx.AvgCloudCoverMid,
				AvgCloudHigh:  wx.AvgCloudCoverHigh,
				AvgWindSpeed:  wx.AvgWindSpeed,
				MaxWindGusts:  gustMax,
				MaxPrecipProb: maxPrecip,
				AvgHumidity:   wx.AvgHumidity,
				AvgDewPoint:   wx.AvgDewPoint,
				AvgTemp:       wx.AvgTemperature,
				AvgVisibility: wx.AvgVisibility,
				Weather:       wx,
			})
		}
	}

	return summaries
}

func fetchWeeklyPressureLevel(lat, lon float64, tz string) (*PressureLevelResponse, error) {
	url := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%.2f&longitude=%.2f"+
			"&hourly=cloud_cover_300hPa,cloud_cover_250hPa,cloud_cover_200hPa,cloud_cover_150hPa,"+
			"relative_humidity_300hPa,relative_humidity_250hPa,relative_humidity_200hPa,"+
			"wind_speed_250hPa"+
			"&models=gfs_seamless&forecast_days=8&timezone=%s",
		lat, lon, tz,
	)

	resp, err := meteoGet(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("pressure-level returned %d: %s", resp.StatusCode, string(body))
	}

	var data PressureLevelResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	return &data, nil
}

func fetchWeeklyIWV(lat, lon float64, tz string) (*ECMWFResponse, error) {
	url := fmt.Sprintf(
		"https://api.open-meteo.com/v1/ecmwf?latitude=%.2f&longitude=%.2f"+
			"&hourly=total_column_integrated_water_vapour"+
			"&forecast_days=8&timezone=%s",
		lat, lon, tz,
	)

	resp, err := meteoGet(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ecmwf weekly returned %d: %s", resp.StatusCode, string(body))
	}

	var data ECMWFResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	return &data, nil
}

func fetchWeeklyAOD(lat, lon float64, tz string) (*AirQualityResponse, error) {
	url := fmt.Sprintf(
		"https://air-quality-api.open-meteo.com/v1/air-quality?latitude=%.2f&longitude=%.2f"+
			"&hourly=aerosol_optical_depth"+
			"&forecast_days=8&timezone=%s",
		lat, lon, tz,
	)

	resp, err := meteoGet(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("air-quality weekly returned %d: %s", resp.StatusCode, string(body))
	}

	var data AirQualityResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	return &data, nil
}

func extractPressureForWindow(wx *NighttimeWeather, data *PressureLevelResponse, loc *time.Location, start, end time.Time) {
	var cc300, cc250, cc200, cc150, rh300, rh250, rh200, ws250 float64
	var count float64

	for i, ts := range data.Hourly.Time {
		t, err := time.ParseInLocation("2006-01-02T15:04", ts, loc)
		if err != nil {
			continue
		}
		if (t.Equal(start) || t.After(start)) && t.Before(end) {
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
		return
	}

	wx.CloudCover300hPa = cc300 / count
	wx.CloudCover250hPa = cc250 / count
	wx.CloudCover200hPa = cc200 / count
	wx.CloudCover150hPa = cc150 / count
	wx.RH300hPa = rh300 / count
	wx.RH250hPa = rh250 / count
	wx.RH200hPa = rh200 / count
	wx.JetStreamSpeed = ws250 / count
}

func extractIWVForWindow(wx *NighttimeWeather, data *ECMWFResponse, loc *time.Location, start, end time.Time) {
	var total float64
	var count float64

	for i, ts := range data.Hourly.Time {
		t, err := time.ParseInLocation("2006-01-02T15:04", ts, loc)
		if err != nil {
			continue
		}
		if (t.Equal(start) || t.After(start)) && t.Before(end) {
			total += data.Hourly.IWV[i]
			count++
		}
	}

	if count > 0 {
		wx.AvgIWV = total / count
	}
}

func extractAODForWindow(wx *NighttimeWeather, data *AirQualityResponse, loc *time.Location, start, end time.Time) {
	var total float64
	var count float64

	for i, ts := range data.Hourly.Time {
		t, err := time.ParseInLocation("2006-01-02T15:04", ts, loc)
		if err != nil {
			continue
		}
		if (t.Equal(start) || t.After(start)) && t.Before(end) {
			total += data.Hourly.AOD[i]
			count++
		}
	}

	if count > 0 {
		wx.AvgAOD = total / count
	}
}

// FullScore produces a score using the same logic as the nightly forecast.
// Requires the NightSummary to have a populated Weather field.
func FullScore(n NightSummary) float64 {
	if n.Weather == nil {
		return QuickScore(n)
	}

	wx := n.Weather

	cloudScore := scoreCloudForWeekly(wx)
	transScore := scoreTransForWeekly(wx)
	humidityScore := scoreHumidityForWeekly(wx)
	precipScore := scorePrecipForWeekly(wx.MaxPrecipProb)
	windScore := scoreWindForWeekly(wx.AvgWindSpeed)
	moonScore := 5.0 // neutral when no moon data

	return cloudScore*0.40 +
		transScore*0.25 +
		moonScore*0.15 +
		humidityScore*0.10 +
		windScore*0.05 +
		precipScore*0.05
}

func scoreCloudForWeekly(wx *NighttimeWeather) float64 {
	lowScore := 10 * (1 - wx.AvgCloudCoverLow/100)
	midScore := 10 * (1 - wx.AvgCloudCoverMid/100)

	// Estimate opacity from pressure-level data
	opacityFactor := 0.5 // default moderate assumption
	saturated := 0
	levels := []float64{wx.CloudCover300hPa, wx.CloudCover250hPa, wx.CloudCover200hPa, wx.CloudCover150hPa}
	for _, cc := range levels {
		if cc > 70 {
			saturated++
		}
	}
	switch {
	case saturated >= 3:
		opacityFactor = 0.95
	case saturated >= 2:
		opacityFactor = 0.70
	case saturated == 1:
		opacityFactor = 0.40
	case saturated == 0 && wx.CloudCover300hPa > 0:
		opacityFactor = 0.25
	}

	if wx.AvgCloudCoverHigh > 50 && wx.AvgVisibility > 0 && wx.AvgVisibility < 10000 {
		if opacityFactor < 0.7 {
			opacityFactor = 0.7
		}
	}

	highScore := 10 * (1 - wx.AvgCloudCoverHigh/100*opacityFactor)
	return lowScore*0.45 + midScore*0.30 + highScore*0.25
}

func scoreTransForWeekly(wx *NighttimeWeather) float64 {
	if wx.AvgIWV <= 0 && wx.AvgAOD <= 0 {
		// No transparency data, use humidity as proxy
		switch {
		case wx.AvgHumidity < 50:
			return 8
		case wx.AvgHumidity < 65:
			return 6
		case wx.AvgHumidity < 80:
			return 4
		default:
			return 2
		}
	}

	iwvScore := 5.0
	if wx.AvgIWV > 0 {
		switch {
		case wx.AvgIWV < 10:
			iwvScore = 10
		case wx.AvgIWV < 15:
			iwvScore = 8
		case wx.AvgIWV < 20:
			iwvScore = 6
		case wx.AvgIWV < 25:
			iwvScore = 4
		case wx.AvgIWV < 35:
			iwvScore = 2
		default:
			iwvScore = 0
		}
	}

	aodScore := 5.0
	if wx.AvgAOD > 0 {
		switch {
		case wx.AvgAOD < 0.05:
			aodScore = 10
		case wx.AvgAOD < 0.10:
			aodScore = 8
		case wx.AvgAOD < 0.15:
			aodScore = 6
		case wx.AvgAOD < 0.25:
			aodScore = 4
		case wx.AvgAOD < 0.40:
			aodScore = 2
		default:
			aodScore = 0
		}
	}

	return iwvScore*0.60 + aodScore*0.40
}

func scoreHumidityForWeekly(wx *NighttimeWeather) float64 {
	spread := wx.AvgTemperature - wx.AvgDewPoint
	switch {
	case spread > 10:
		return 10
	case spread > 7:
		return 8
	case spread > 5:
		return 6
	case spread > 3:
		return 4
	case spread > 1.5:
		return 2
	default:
		return 0
	}
}

func scorePrecipForWeekly(maxProb float64) float64 {
	if maxProb <= 0 {
		return 10
	}
	if maxProb >= 50 {
		return 0
	}
	return 10 * (1 - maxProb/50)
}

func scoreWindForWeekly(avgSpeed float64) float64 {
	switch {
	case avgSpeed < 10:
		return 10
	case avgSpeed < 20:
		return 10 - (avgSpeed-10)*0.4
	case avgSpeed < 30:
		return 6 - (avgSpeed-20)*0.3
	default:
		return 0
	}
}

// QuickScore produces a simplified 0-10 score as a fallback when full weather
// data is not available for a night.
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
