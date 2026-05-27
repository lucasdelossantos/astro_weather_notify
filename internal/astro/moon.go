package astro

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type USNOResponse struct {
	Properties USNOProperties `json:"properties"`
}

type USNOProperties struct {
	Data USNOData `json:"data"`
}

type USNOData struct {
	SunData      []USNOPhenomenon `json:"sundata"`
	MoonData     []USNOPhenomenon `json:"moondata"`
	CurPhase     string           `json:"curphase"`
	FracIllum    string           `json:"fracillum"`
	ClosestPhase USNOPhase        `json:"closestphase"`
}

type USNOPhenomenon struct {
	Phen string `json:"phen"`
	Time string `json:"time"`
}

type USNOPhase struct {
	Phase string `json:"phase"`
	Date  string `json:"date"`
	Time  string `json:"time"`
}

type MoonInfo struct {
	Phase        string
	Illumination float64
	Moonrise     string
	Moonset      string
	Sunset       string
	Sunrise      string
	CivilTwilightEnd   string // evening civil twilight end
	CivilTwilightBegin string // morning civil twilight begin
	Available    bool
}

// FetchMoonInfo fetches moon phase, illumination, and rise/set times from USNO.
func FetchMoonInfo(lat, lon float64, tz string) (*MoonInfo, error) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone: %w", err)
	}

	now := time.Now().In(loc)
	date := now.Format("2006-01-02")

	_, offset := now.Zone()
	tzHours := offset / 3600

	url := fmt.Sprintf(
		"https://aa.usno.navy.mil/api/rstt/oneday?date=%s&coords=%.2f,%.2f&tz=%d",
		date, lat, lon, tzHours,
	)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return fallbackMoonInfo(now), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		_ = body
		return fallbackMoonInfo(now), nil
	}

	var data USNOResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return fallbackMoonInfo(now), nil
	}

	info := &MoonInfo{
		Phase:     data.Properties.Data.CurPhase,
		Available: true,
	}

	if illum := data.Properties.Data.FracIllum; illum != "" {
		illum = strings.TrimSuffix(illum, "%")
		if v, err := strconv.ParseFloat(illum, 64); err == nil {
			info.Illumination = v
		}
	}

	for _, p := range data.Properties.Data.MoonData {
		switch p.Phen {
		case "Rise", "R":
			info.Moonrise = p.Time
		case "Set", "S":
			info.Moonset = p.Time
		}
	}

	for _, p := range data.Properties.Data.SunData {
		switch p.Phen {
		case "Rise", "R":
			info.Sunrise = p.Time
		case "Set", "S":
			info.Sunset = p.Time
		case "End Civil Twilight", "EC":
			info.CivilTwilightEnd = p.Time
		case "Begin Civil Twilight", "BC":
			info.CivilTwilightBegin = p.Time
		}
	}

	return info, nil
}

// fallbackMoonInfo computes approximate moon phase algorithmically when USNO is unavailable.
func fallbackMoonInfo(now time.Time) *MoonInfo {
	// Simple synodic month calculation
	// Known new moon: Jan 6, 2000 18:14 UTC
	ref := time.Date(2000, 1, 6, 18, 14, 0, 0, time.UTC)
	days := now.Sub(ref).Hours() / 24.0
	synodicMonth := 29.53058770576
	phase := days / synodicMonth
	phase = phase - float64(int(phase)) // fractional part
	if phase < 0 {
		phase += 1
	}

	// Illumination approximation: 0 at new, 1 at full
	illumination := (1 - (1+cosApprox(phase*2*3.14159265))/2) * 100

	var phaseName string
	switch {
	case phase < 0.0625:
		phaseName = "New Moon"
	case phase < 0.1875:
		phaseName = "Waxing Crescent"
	case phase < 0.3125:
		phaseName = "First Quarter"
	case phase < 0.4375:
		phaseName = "Waxing Gibbous"
	case phase < 0.5625:
		phaseName = "Full Moon"
	case phase < 0.6875:
		phaseName = "Waning Gibbous"
	case phase < 0.8125:
		phaseName = "Last Quarter"
	case phase < 0.9375:
		phaseName = "Waning Crescent"
	default:
		phaseName = "New Moon"
	}

	return &MoonInfo{
		Phase:        phaseName,
		Illumination: illumination,
		Available:    true,
	}
}

func cosApprox(x float64) float64 {
	// Taylor series approximation for cos, adequate for this use
	x = x - float64(int(x/(2*3.14159265)))*2*3.14159265
	if x < 0 {
		x = -x
	}
	x2 := x * x
	return 1 - x2/2 + x2*x2/24 - x2*x2*x2/720
}
