# Plan: Rework Forecast Accuracy with Cloud Opacity and Enhanced Data

## Executive Summary

The current scoring system gave a "Good Night to Shoot" verdict on a night with 100% high cloud cover that produced a completely unusable sky (moon glazed over, zero stars visible). The root cause is twofold: (1) high clouds are treated as nearly harmless with no ability to distinguish thin cirrus from thick cirrostratus, and (2) several critical atmospheric quality metrics are either not fetched or not used in scoring. This rework adds cloud thickness estimation via pressure-level vertical profiles, integrates water vapor and aerosol data for transparency assessment, and reweights the scoring to match what actually matters for wide-field astrophotography.

## Deep Research Findings

### Current Data Flow

```
Open-Meteo (single call) -> cloud_cover_{low,mid,high}, visibility, humidity, dew_point, wind, precip
7Timer                    -> seeing (1-8), transparency (1-8)
```

### What's Wrong

1. **Cloud opacity is unknown** -- `cloud_cover_high = 100%` tells us the sky is covered but not whether those clouds are thin (tau ~0.1, shootable) or thick (tau >1.0, opaque). The 30% penalty rate assumes all high clouds are thin cirrus.

2. **Visibility is fetched but ignored** -- `AvgVisibility` is stored in `NighttimeWeather` but never used in scoring. Low visibility + high clouds = thick opaque layer. High visibility + high clouds = thin cirrus.

3. **No vertical cloud profile** -- Open-Meteo offers pressure-level cloud cover (300, 250, 200, 150 hPa). Multiple saturated levels = geometrically thick cloud. Single level = thin layer.

4. **No water vapor column** -- Total column integrated water vapor (IWV) from ECMWF directly correlates with atmospheric transparency. <15 kg/m^2 = good, >25 = poor.

5. **No aerosol data** -- Smoke, haze, and particulates destroy contrast for wide-field imaging. CAMS provides aerosol optical depth (AOD) via Open-Meteo's air quality API.

6. **No jet stream awareness** -- Wind speed at 250 hPa indicates jet stream overhead. >30 m/s = high cirrus risk even if current coverage looks clear.

7. **Seeing is overweighted** -- 20% of the score goes to a metric that is irrelevant for wide-field imaging on a DSLR (pixel scale ~30 arcsec/pixel at 18mm).

8. **Dew risk assessment is crude** -- Uses humidity alone, but dew point spread (temp minus dew point) is the real indicator. Without a dew heater, the user is vulnerable at spreads <4C.

### Dependencies Traced

- `scoring.Generate()` <- called from `cmd/notify/main.go:372`
- `scoring.Generate()` takes `*weather.NighttimeWeather` and `*weather.AstroConditions`
- `weather.FetchNighttimeWeather()` <- called from `cmd/notify/main.go:347`
- `weather.QuickScore()` <- called from `cmd/notify/main.go:205` (weekly embed)
- `discord.BuildEmbed()` <- reads from `scoring.Report` struct
- `discord.formatSkyConditions()` <- displays cloud/seeing/transparency/humidity/wind

### Affected Components

- `internal/weather/openmeteo.go` -- new API calls, new struct fields
- `internal/weather/weekly.go` -- `QuickScore` uses same broken cloud logic
- `internal/scoring/score.go` -- complete scoring rework
- `internal/discord/webhook.go` -- display new metrics (opacity, transparency source, dew spread)
- `cmd/notify/main.go` -- wire new data fetches into report generation

---

## Numbered Changes

### 1. New Data Source: Pressure-Level Cloud Profile

**Location:** `internal/weather/openmeteo.go`

**What:** Add a second Open-Meteo request (or extend the existing one) to fetch pressure-level data for cirrus-level altitudes.

**Variables to add:**
- `cloud_cover_300hPa` (9.2 km -- cirrus base)
- `cloud_cover_250hPa` (10.4 km -- mid-cirrus)
- `cloud_cover_200hPa` (11.8 km -- upper cirrus)
- `cloud_cover_150hPa` (13.6 km -- high cirrus)
- `relative_humidity_300hPa`
- `relative_humidity_250hPa`
- `relative_humidity_200hPa`
- `wind_speed_250hPa` (jet stream indicator)

**Model:** Use `models=gfs_seamless` since pressure-level data availability varies by model.

**Why:** Counting how many consecutive pressure levels show cloud cover >70% or RH >85% gives a proxy for cloud geometric thickness. 1 level = thin, 3-4 levels = thick/opaque.

### 2. New Data Source: ECMWF Water Vapor Column

**Location:** `internal/weather/openmeteo.go` (new function)

**What:** Fetch `total_column_integrated_water_vapour` from the ECMWF endpoint.

**Endpoint:** `https://api.open-meteo.com/v1/ecmwf?latitude=X&longitude=Y&hourly=total_column_integrated_water_vapour&forecast_days=2&timezone=Z`

**Why:** IWV is the single best predictor of atmospheric transparency from model data. Thresholds:
- <10 kg/m^2: Excellent transparency
- 10-15: Good
- 15-25: Average
- >25: Poor (hazy even without clouds)

### 3. New Data Source: Aerosol Optical Depth

**Location:** `internal/weather/openmeteo.go` (new function)

**What:** Fetch `aerosol_optical_depth` from the Open-Meteo air quality API.

**Endpoint:** `https://air-quality-api.open-meteo.com/v1/air-quality?latitude=X&longitude=Y&hourly=aerosol_optical_depth&forecast_days=2&timezone=Z`

**Why:** Smoke and haze destroy wide-field contrast. AOD thresholds:
- <0.05: Excellent
- 0.05-0.15: Good
- 0.15-0.30: Noticeable haze
- >0.30: Poor -- significant extinction

### 4. Compute Cloud Opacity Score

**Location:** `internal/scoring/score.go` (new function)

**What:** Replace the naive `scoreCloudLayers()` with a function that accounts for thickness:

```go
func scoreCloudOpacity(low, mid, high float64, thickness CloudThickness, visibility float64) float64 {
    // Low and mid clouds are always opaque -- penalty is straightforward
    lowScore := 10 * (1 - low/100)
    midScore := 10 * (1 - mid/100)

    // High cloud penalty scales with estimated opacity
    highOpacity := estimateHighCloudOpacity(high, thickness, visibility)
    highScore := 10 * (1 - high/100*highOpacity)

    return lowScore*0.45 + midScore*0.30 + highScore*0.25
}
```

The `estimateHighCloudOpacity()` function combines:
- **Vertical extent** (pressure levels with clouds): 1 level -> opacity factor 0.3, 2 levels -> 0.6, 3+ levels -> 0.9
- **Visibility cross-check**: if visibility <10km with high clouds, increase opacity estimate
- **RH saturation depth**: consecutive levels with RH >85% confirms thick cloud

This means 100% high clouds that are thin cirrus (1 level, good visibility) still get a moderate penalty (~0.3 factor), but 100% high clouds that are thick cirrostratus (3+ levels, low visibility) get treated nearly as harshly as mid-layer clouds (~0.9 factor).

### 5. Compute Transparency Score from Physical Data

**Location:** `internal/scoring/score.go` (new function)

**What:** Replace reliance on 7Timer's coarse 1-8 scale with a transparency score derived from IWV + AOD + high cloud opacity:

```go
func scoreTransparency(iwv float64, aod float64, highCloudOpacity float64, highCover float64, sevenTimerTrans int, sevenTimerAvailable bool) float64 {
    // Physical model: combine water vapor extinction + aerosol extinction + cloud scatter
    iwvScore := scoreIWV(iwv)        // 0-10 based on thresholds above
    aodScore := scoreAOD(aod)        // 0-10 based on thresholds above
    cloudExtinction := highCloudOpacity * (highCover / 100) * 2.0  // additional scatter from thin clouds

    // Weighted: IWV dominates, AOD secondary, thin cloud scatter tertiary
    physicalScore := iwvScore*0.50 + aodScore*0.30 - cloudExtinction
    if physicalScore < 0 {
        physicalScore = 0
    }

    // If 7Timer is available, blend it in as a sanity check (20% weight)
    if sevenTimerAvailable {
        sevenTimerScore := 10 * (1 - float64(sevenTimerTrans-1)/7)
        return physicalScore*0.80 + sevenTimerScore*0.20
    }
    return physicalScore
}
```

**Why:** 7Timer uses GFS at 28km resolution -- coarse and often wrong. Physical parameters (IWV, AOD) from ECMWF and CAMS are more reliable and tell us WHY transparency is bad, not just that it is.

### 6. Rework Overall Score Weights

**Location:** `internal/scoring/score.go`, `Generate()` function

**Current weights:**
| Component | Weight |
|---|---|
| Cloud layers | 35% |
| Seeing | 20% |
| Transparency | 20% |
| Moon | 15% |
| Precip | 5% |
| Wind | 5% |

**New weights:**
| Component | Weight | Rationale |
|---|---|---|
| Cloud opacity | 40% | Absolute gatekeeper -- if sky is blocked, nothing else matters |
| Transparency | 25% | Critical for wide-field contrast and limiting magnitude |
| Moon | 15% | Hard constraint for deep-sky/Milky Way |
| Humidity/Dew | 10% | Equipment risk without dew heater; also degrades transparency |
| Wind | 5% | Tripod stability at longer focal lengths |
| Precip | 5% | Equipment safety |

**Seeing is removed from the score.** It is still fetched and displayed as informational (useful if you get a tracked scope later), but it does not affect the go/no-go number because wide-field imaging is unaffected by seeing under ~10 arcsec.

### 7. Add Humidity/Dew Score

**Location:** `internal/scoring/score.go` (new function)

**What:** Proper dew risk scoring using dew point spread:

```go
func scoreHumidity(humidity, dewPoint, temperature float64) float64 {
    spread := temperature - dewPoint
    switch {
    case spread > 10:
        return 10  // very safe
    case spread > 7:
        return 8   // safe
    case spread > 5:
        return 6   // low risk
    case spread > 3:
        return 4   // moderate risk, mention in notes
    case spread > 1.5:
        return 2   // high risk, dew likely on unprotected optics
    default:
        return 0   // fogging almost certain
    }
}
```

**Why:** Current code only uses humidity percentage, which is a rough proxy. Dew point spread directly predicts when condensation forms on optical surfaces that radiate heat to the sky (dropping 2-5C below ambient).

**Note:** We need temperature data. Open-Meteo has `temperature_2m` available -- just not currently requested.

### 8. Add Jet Stream Risk Warning

**Location:** `internal/scoring/score.go` (new field in Report)

**What:** When `wind_speed_250hPa` > 30 m/s, add a warning flag that cirrus may develop or worsen even if current cloud cover looks clear. This does not directly penalize the score (models already predict the clouds) but adds a confidence/uncertainty note to the recommendation.

### 9. Update NighttimeWeather Struct

**Location:** `internal/weather/openmeteo.go`

**New fields:**
```go
type NighttimeWeather struct {
    // ... existing fields ...
    AvgTemperature       float64

    // Pressure-level cloud profile (high atmosphere)
    CloudCover300hPa     float64
    CloudCover250hPa     float64
    CloudCover200hPa     float64
    CloudCover150hPa     float64
    RH300hPa             float64
    RH250hPa             float64
    RH200hPa             float64
    JetStreamSpeed       float64  // wind at 250 hPa, m/s

    // ECMWF transparency data
    AvgIWV               float64  // integrated water vapor, kg/m^2

    // CAMS air quality
    AvgAOD               float64  // aerosol optical depth at 550nm
}
```

### 10. Update Report Struct

**Location:** `internal/scoring/score.go`

**New/modified fields:**
```go
type Report struct {
    // ... existing ...
    CloudOpacity     string   // "Thin Cirrus", "Moderate", "Thick/Opaque"
    TransparencyDesc string   // now based on physical data (IWV + AOD)
    DewPointSpread   float64  // actual temp - dewpoint in C
    DewRiskNote      string   // setup-specific: "No dew heater -- check lens every 15min"
    JetStreamRisk    bool     // true when 250hPa wind > 30 m/s
    IWV              float64  // for display
    AOD              float64  // for display
    Confidence       string   // "High" / "Moderate" / "Low" based on model agreement + jet stream

    // Keep seeing for display but remove from score
    Seeing     int
    SeeingDesc string
    SeeingNote string  // "Seeing does not affect wide-field imaging with your setup"
}
```

### 11. Update Discord Embed Display

**Location:** `internal/discord/webhook.go`

**Changes to `formatSkyConditions()`:**
- Show cloud opacity estimate alongside coverage: "High: 95% (Thick -- 3 saturated levels)"
- Show transparency source: "Transparency: Good (IWV: 12 kg/m^2, AOD: 0.04)"
- Show dew point spread: "Dew Risk: Moderate (spread 3.5C) -- no heater, check lens every 15min"
- Show jet stream warning when active
- Show seeing as informational only with note it does not affect score

### 12. Update Weekly Quick Score

**Location:** `internal/weather/weekly.go`

**What:** `QuickScore()` currently uses the same broken 30% high cloud penalty. Update to use a more conservative default penalty. For the weekly forecast we will not have pressure-level data (too many API calls for 7 days), so:
- Increase the base high cloud penalty from 0.3 to 0.7 (assume moderate opacity when thickness is unknown)
- Add humidity into the weekly score as a transparency proxy

### 13. Add Temperature to Open-Meteo Request

**Location:** `internal/weather/openmeteo.go`

**What:** Add `temperature_2m` to the hourly parameters in the existing forecast URL. Needed for proper dew point spread calculation.

---

## Summary Table

| # | File | Change | Priority |
|---|---|---|---|
| 1 | weather/openmeteo.go | Add pressure-level cloud + RH + jet stream fetch | High |
| 2 | weather/openmeteo.go | Add ECMWF IWV fetch (new function) | High |
| 3 | weather/openmeteo.go | Add CAMS AOD fetch (new function) | High |
| 4 | scoring/score.go | Cloud opacity scoring with thickness estimate | High |
| 5 | scoring/score.go | Physical transparency score (IWV + AOD) | High |
| 6 | scoring/score.go | Reweight final score (remove seeing, add humidity/dew) | High |
| 7 | scoring/score.go | Dew point spread scoring | Medium |
| 8 | scoring/score.go | Jet stream risk flag | Medium |
| 9 | weather/openmeteo.go | Expand NighttimeWeather struct | High |
| 10 | scoring/score.go | Expand Report struct | High |
| 11 | discord/webhook.go | Update embed display for new metrics | Medium |
| 12 | weather/weekly.go | Fix QuickScore high cloud penalty | Medium |
| 13 | weather/openmeteo.go | Add temperature_2m to request | High |

**Dependency order:** 9, 13 -> 1, 2, 3 -> 4, 5, 7 -> 6, 8, 10 -> 11, 12

---

## Recommended Implementation Order

**Phase 1: Data layer** (changes 9, 13, 1, 2, 3)
- Expand structs and API calls to fetch all new data
- Existing scoring still works (new fields just unused initially)
- Can test each API call independently

**Phase 2: Scoring rework** (changes 4, 5, 6, 7, 8, 10)
- Replace scoring functions
- This is where the behavior change happens
- Single commit so the transition is atomic

**Phase 3: Display and weekly** (changes 11, 12)
- Update Discord embed to show new data
- Fix weekly quick score

---

## Risks and Considerations

1. **API rate limits** -- Three separate Open-Meteo calls per forecast (weather, ECMWF, air quality). Open-Meteo's free tier allows 10,000 calls/day. We make ~3 calls per forecast invocation, well within limits.

2. **ECMWF data availability** -- The ECMWF endpoint has slightly different temporal coverage than the standard forecast. May not always have data for tonight's exact window. Need graceful fallback (use neutral score).

3. **AOD data gaps** -- CAMS air quality updates less frequently and coverage may vary. Need fallback to assumed-good (0.08) when unavailable.

4. **Pressure-level data model availability** -- Not all Open-Meteo models expose pressure-level variables. Using `models=gfs_seamless` or `best_match` should work. Need to verify in integration testing.

5. **Backward compatibility** -- The Report struct gains new fields. Discord embed formatting must handle zero-value fields gracefully (if a fetch fails, don't display garbage -- show "unavailable" or omit).

6. **Validation scenario** -- After implementation, replay the bad-forecast case: 100% high cloud, thick cirrostratus. Expected result with new system: 3+ pressure levels saturated -> opacity factor ~0.9 -> high cloud penalty becomes (100% * 0.9) = 90% effective blockage -> cloud score drops to ~2.5/10 -> overall score becomes "Poor" or "Stay Inside". Matches reality.

---

## Code Comments Plan

- `scoreCloudOpacity()`: Document that opacity factor 0.3/0.6/0.9 thresholds are derived from observed correlation between pressure-level saturation depth and visual sky quality
- `estimateHighCloudOpacity()`: Document the three inputs (vertical extent, visibility, RH depth) and why each matters
- `scoreTransparency()`: Document the IWV/AOD threshold sources (observational astronomy practice)
- `scoreHumidity()`: Document that optical surfaces radiate 2-5C below ambient, making dew thresholds lower than standard meteorological expectations
- Jet stream threshold: 30 m/s at 250 hPa correlates with cirrus generation from wind shear

---

Planning document complete. Please review. I will proceed with implementation after your approval.
