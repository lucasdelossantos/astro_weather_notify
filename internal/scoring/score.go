package scoring

import (
	"fmt"
	"time"

	"github.com/ldelossa/astro_weather_notify/internal/astro"
	"github.com/ldelossa/astro_weather_notify/internal/weather"
)

type Report struct {
	Score          float64
	Verdict        string
	CloudCoverPct  float64
	CloudCoverLow  float64
	CloudCoverMid  float64
	CloudCoverHigh float64
	CloudDesc      string
	CloudOpacity   string
	Seeing         int
	SeeingDesc     string
	SeeingNote     string
	Transparency   int
	TransDesc      string
	Humidity       float64
	DewPointSpread float64
	DewRiskNote    string
	WindSpeed      float64
	WindGusts      float64
	PrecipProb     float64
	Moon           *astro.MoonInfo
	MoonImpact     string
	Planets        []astro.PlanetInfo
	Targets        []astro.Target
	Aurora         *astro.AuroraForecast
	Recommendation string
	JetStreamRisk  bool
	JetStreamSpeed float64
	IWV            float64
	AOD            float64
	Visibility     float64
}

// CloudThickness represents the estimated vertical extent of high clouds.
type CloudThickness struct {
	SaturatedLevels int     // how many pressure levels show cloud cover >70%
	RHLevels        int     // how many pressure levels show RH >85%
	OpacityFactor   float64 // 0-1 estimated opacity (0=transparent, 1=opaque)
}

// Generate produces a full scoring report from weather and astro data.
func Generate(wx *weather.NighttimeWeather, astroWx *weather.AstroConditions, moon *astro.MoonInfo, planets []astro.PlanetInfo) *Report {
	r := &Report{
		CloudCoverPct:  wx.AvgCloudCover,
		CloudCoverLow:  wx.AvgCloudCoverLow,
		CloudCoverMid:  wx.AvgCloudCoverMid,
		CloudCoverHigh: wx.AvgCloudCoverHigh,
		Humidity:       wx.AvgHumidity,
		DewPointSpread: wx.AvgTemperature - wx.AvgDewPoint,
		WindSpeed:      wx.AvgWindSpeed,
		WindGusts:      wx.MaxWindGusts,
		PrecipProb:     wx.MaxPrecipProb,
		Visibility:     wx.AvgVisibility,
		Moon:           moon,
		Planets:        planets,
		IWV:            wx.AvgIWV,
		AOD:            wx.AvgAOD,
		JetStreamSpeed: wx.JetStreamSpeed,
		JetStreamRisk:  wx.JetStreamSpeed > 30,
	}

	if astroWx.Available {
		r.Seeing = astroWx.Seeing
		r.Transparency = astroWx.Transparency
	}

	// Estimate high cloud thickness from pressure-level vertical profile
	thickness := estimateCloudThickness(wx)
	r.CloudOpacity = cloudOpacityDescription(thickness)

	// Score components (each normalized to 0-10)
	cloudScore := scoreCloudOpacity(wx.AvgCloudCoverLow, wx.AvgCloudCoverMid, wx.AvgCloudCoverHigh, thickness, wx.AvgVisibility)
	transScore := scoreTransparency(wx.AvgIWV, wx.AvgAOD, thickness.OpacityFactor, wx.AvgCloudCoverHigh, r.Transparency, astroWx.Available)
	moonScore := scoreMoon(moon)
	humidityScore := scoreHumidity(wx.AvgHumidity, wx.AvgDewPoint, wx.AvgTemperature)
	precipScore := scorePrecip(wx.MaxPrecipProb)
	windScore := scoreWind(wx.AvgWindSpeed)

	// Weighted combination — seeing removed (irrelevant for wide-field DSLR),
	// cloud opacity and transparency dominate since they are the go/no-go factors.
	r.Score = cloudScore*0.40 +
		transScore*0.25 +
		moonScore*0.15 +
		humidityScore*0.10 +
		windScore*0.05 +
		precipScore*0.05

	r.Verdict = verdict(r.Score)
	r.CloudDesc = cloudDescription(wx.AvgCloudCover)
	r.SeeingDesc = seeingDescription(r.Seeing)
	r.SeeingNote = "Seeing does not affect wide-field imaging"
	r.TransDesc = transparencyDescription(r.IWV, r.AOD, r.Transparency, astroWx.Available)
	r.DewRiskNote = dewRiskNote(r.DewPointSpread)
	r.MoonImpact = moonImpactDescription(moon)
	r.Recommendation = generateRecommendation(r)

	moonIllum := 0.0
	if moon != nil && moon.Available {
		moonIllum = moon.Illumination
	}
	r.Targets = astro.SuggestTargets(time.Now().Month(), moonIllum, r.Score)

	return r
}

// estimateCloudThickness uses pressure-level cloud cover and RH to determine
// whether high clouds are thin (single layer cirrus) or thick (deep cirrostratus).
func estimateCloudThickness(wx *weather.NighttimeWeather) CloudThickness {
	ct := CloudThickness{}

	// Count pressure levels with significant cloud cover (>70%)
	levels := []float64{wx.CloudCover300hPa, wx.CloudCover250hPa, wx.CloudCover200hPa, wx.CloudCover150hPa}
	for _, cc := range levels {
		if cc > 70 {
			ct.SaturatedLevels++
		}
	}

	// Count pressure levels with high relative humidity (>85%) confirming moisture depth
	rhLevels := []float64{wx.RH300hPa, wx.RH250hPa, wx.RH200hPa}
	for _, rh := range rhLevels {
		if rh > 85 {
			ct.RHLevels++
		}
	}

	// Derive opacity factor from combined evidence
	switch {
	case ct.SaturatedLevels >= 3 || ct.RHLevels >= 3:
		ct.OpacityFactor = 0.95
	case ct.SaturatedLevels >= 2 || ct.RHLevels >= 2:
		ct.OpacityFactor = 0.70
	case ct.SaturatedLevels == 1 || ct.RHLevels == 1:
		ct.OpacityFactor = 0.40
	default:
		// No pressure-level data or all clear — fall back to visibility heuristic
		ct.OpacityFactor = 0.25
	}

	return ct
}

// scoreCloudOpacity replaces the old scoreCloudLayers. High cloud penalty now
// scales with estimated thickness rather than using a fixed 30% discount.
func scoreCloudOpacity(low, mid, high float64, thickness CloudThickness, visibility float64) float64 {
	lowScore := 10 * (1 - low/100)
	midScore := 10 * (1 - mid/100)

	// High cloud penalty scaled by opacity factor.
	// Thin cirrus (factor 0.25) gets a light penalty; thick cirrostratus (0.95) is nearly opaque.
	opacityFactor := thickness.OpacityFactor

	// Visibility cross-check: low visibility with high clouds confirms thick layer
	if high > 50 && visibility > 0 && visibility < 10000 {
		if opacityFactor < 0.7 {
			opacityFactor = 0.7
		}
	}

	highScore := 10 * (1 - high/100*opacityFactor)

	// Weights: low still dominates (opaque), high elevated from old 10% since
	// we can now properly assess its impact.
	return lowScore*0.45 + midScore*0.30 + highScore*0.25
}

func scoreIWV(iwv float64) float64 {
	if iwv <= 0 {
		return 5 // no data available, neutral
	}
	switch {
	case iwv < 10:
		return 10
	case iwv < 15:
		return 8
	case iwv < 20:
		return 6
	case iwv < 25:
		return 4
	case iwv < 35:
		return 2
	default:
		return 0
	}
}

func scoreAOD(aod float64) float64 {
	if aod <= 0 {
		return 5 // no data, neutral
	}
	switch {
	case aod < 0.05:
		return 10
	case aod < 0.10:
		return 8
	case aod < 0.15:
		return 6
	case aod < 0.25:
		return 4
	case aod < 0.40:
		return 2
	default:
		return 0
	}
}

// scoreTransparency combines physical data (IWV, AOD, cloud scatter) with
// 7Timer as a secondary sanity check when available.
func scoreTransparency(iwv, aod, highOpacity, highCover float64, sevenTimerTrans int, sevenTimerAvailable bool) float64 {
	iwvScore := scoreIWV(iwv)
	aodScore := scoreAOD(aod)

	// Thin cloud scatter penalty: even "thin" high clouds scatter light and reduce contrast
	cloudExtinction := highOpacity * (highCover / 100) * 2.5
	if cloudExtinction > 5 {
		cloudExtinction = 5
	}

	physicalScore := iwvScore*0.55 + aodScore*0.30 - cloudExtinction
	if physicalScore < 0 {
		physicalScore = 0
	}
	if physicalScore > 10 {
		physicalScore = 10
	}

	hasPhysicalData := iwv > 0 || aod > 0

	if hasPhysicalData && sevenTimerAvailable {
		sevenTimerScore := 10 * (1 - float64(sevenTimerTrans-1)/7)
		return physicalScore*0.75 + sevenTimerScore*0.25
	}
	if hasPhysicalData {
		return physicalScore
	}
	if sevenTimerAvailable {
		return 10 * (1 - float64(sevenTimerTrans-1)/7)
	}
	return 5 // no data at all, neutral
}

// scoreHumidity uses dew point spread to assess condensation risk.
// Optical surfaces radiate 2-5C below ambient, so dew forms before the
// meteorological dew point is reached at air temperature.
func scoreHumidity(humidity, dewPoint, temperature float64) float64 {
	spread := temperature - dewPoint
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

func scoreMoon(moon *astro.MoonInfo) float64 {
	if moon == nil || !moon.Available {
		return 5
	}
	return 10 * (1 - moon.Illumination/100)
}

func scorePrecip(maxProb float64) float64 {
	if maxProb <= 0 {
		return 10
	}
	if maxProb >= 50 {
		return 0
	}
	return 10 * (1 - maxProb/50)
}

func scoreWind(avgSpeed float64) float64 {
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

func verdict(score float64) string {
	switch {
	case score >= 8:
		return "Excellent Night to Shoot"
	case score >= 6:
		return "Good Night to Shoot"
	case score >= 4:
		return "Marginal - Consider It"
	case score >= 2:
		return "Poor Conditions"
	default:
		return "Stay Inside"
	}
}

func cloudDescription(pct float64) string {
	switch {
	case pct <= 10:
		return "Clear"
	case pct <= 25:
		return "Mostly Clear"
	case pct <= 50:
		return "Partly Cloudy"
	case pct <= 75:
		return "Mostly Cloudy"
	default:
		return "Overcast"
	}
}

func cloudOpacityDescription(ct CloudThickness) string {
	switch {
	case ct.OpacityFactor >= 0.85:
		return "Thick/Opaque"
	case ct.OpacityFactor >= 0.55:
		return "Moderate"
	case ct.OpacityFactor >= 0.30:
		return "Thin Cirrus"
	default:
		return "Clear"
	}
}

func seeingDescription(val int) string {
	switch {
	case val <= 2:
		return "Excellent"
	case val <= 3:
		return "Good"
	case val <= 5:
		return "Average"
	case val <= 6:
		return "Below Average"
	default:
		return "Poor"
	}
}

func transparencyDescription(iwv, aod float64, sevenTimer int, sevenTimerAvailable bool) string {
	if iwv > 0 {
		switch {
		case iwv < 10:
			return "Excellent"
		case iwv < 15:
			return "Good"
		case iwv < 20:
			return "Average"
		case iwv < 25:
			return "Below Average"
		default:
			return "Poor"
		}
	}
	if sevenTimerAvailable {
		switch {
		case sevenTimer <= 2:
			return "Very Good"
		case sevenTimer <= 3:
			return "Good"
		case sevenTimer <= 5:
			return "Average"
		case sevenTimer <= 6:
			return "Below Average"
		default:
			return "Poor"
		}
	}
	return "Unknown"
}

func dewRiskNote(spread float64) string {
	switch {
	case spread > 7:
		return "Low"
	case spread > 4:
		return "Moderate -- monitor lens for condensation"
	case spread > 2:
		return "High -- check lens every 15 min, consider dew shield"
	default:
		return "Very High -- fogging likely, protect optics"
	}
}

func moonImpactDescription(moon *astro.MoonInfo) string {
	if moon == nil || !moon.Available {
		return "Unknown"
	}
	switch {
	case moon.Illumination <= 15:
		return "Minimal - dark skies"
	case moon.Illumination <= 40:
		return "Low - minor sky glow"
	case moon.Illumination <= 60:
		return "Moderate - stick to brighter targets"
	case moon.Illumination <= 80:
		return "High - bright sky background"
	default:
		return "Severe - very bright, deep-sky imaging difficult"
	}
}

func generateRecommendation(r *Report) string {
	if r.Score >= 8 {
		msg := fmt.Sprintf("Clear skies with %s transparency.", r.TransDesc)
		if r.MoonImpact != "" {
			msg += fmt.Sprintf(" %s moon impact.", r.MoonImpact)
		}
		msg += " Excellent conditions for wide-field imaging."
		if r.DewPointSpread < 5 {
			msg += " Watch for dew on your lens as the night progresses."
		}
		return msg
	}
	if r.Score >= 6 {
		msg := "Good conditions overall."
		if r.CloudCoverPct > 20 {
			msg += " Some cloud cover may interrupt sessions."
		}
		if r.CloudCoverHigh > 40 {
			msg += fmt.Sprintf(" High clouds are %s -- may reduce contrast.", r.CloudOpacity)
		}
		if r.Moon != nil && r.Moon.Illumination > 40 {
			msg += " Consider targets away from the moon."
		}
		if r.DewPointSpread < 4 {
			msg += " Dew risk is elevated -- bring a lens cloth."
		}
		return msg
	}
	if r.Score >= 4 {
		msg := "Marginal conditions."
		if r.CloudCoverHigh > 50 {
			msg += fmt.Sprintf(" High cloud cover (%s) will reduce sky quality.", r.CloudOpacity)
		}
		if r.CloudCoverPct > 50 {
			msg += " Heavy cloud cover expected."
		}
		if r.IWV > 25 || r.AOD > 0.2 {
			msg += " Atmospheric transparency is poor."
		}
		if r.Moon != nil && r.Moon.Illumination > 60 {
			msg += " Bright moon washes out faint targets."
		}
		return msg + " Not recommended for a long drive to a dark site."
	}
	if r.Score >= 2 {
		msg := "Poor conditions."
		if r.CloudCoverHigh > 70 {
			msg += fmt.Sprintf(" Sky largely blocked by %s high clouds.", r.CloudOpacity)
		}
		return msg + " Not recommended unless testing equipment nearby."
	}
	return "Conditions are too poor for any useful imaging tonight."
}
