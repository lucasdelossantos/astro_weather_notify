# Equipment Profile System

## Executive Summary

The bot currently hardcodes equipment assumptions (Nikon D3500, untracked, two kit lenses) throughout the target catalog, scoring weights, and recommendation text. This plan introduces a JSON-based equipment profile that drives target selection, camera settings, and scoring behavior. Adding a new lens or rig becomes a config file edit, not a code change. The immediate concrete addition is the ZWO Seestar S30 Pro (tracked smart telescope).

## Deep Research Findings

### Dependencies Traced

- `internal/astro/targets.go` — hardcoded `catalog` slice with `Lens` and `Settings` fields baked for specific glass
- `internal/scoring/score.go:92-97` — scoring weights assume wide-field untracked (seeing excluded at 0% weight)
- `internal/scoring/score.go:102` — `SeeingNote` hardcodes "Seeing does not affect wide-field imaging"
- `internal/scoring/score.go:106` — `generateRecommendation()` references "wide-field imaging"
- `internal/discord/webhook.go:209-215` — `formatTargets()` renders `Lens` and `Settings` fields
- `internal/config/config.go` — no equipment fields exist today
- `cmd/notify/main.go:112` — `SuggestTargets()` call has no equipment context
- `docker-compose.yml` — would need a volume mount for the profile file

### Call Graph

```
main.go → scoring.Generate() → astro.SuggestTargets()
                                    ↓
                              catalog (hardcoded slice)
                                    ↓
                              filtered by month + moon
```

Scoring weights are static constants in `Generate()`. Recommendations are generated from the report struct with no knowledge of equipment.

### Data Flow

Profile JSON → loaded at startup by config package → passed into scoring.Generate() and astro.SuggestTargets() → drives which targets appear, what settings text is shown, and how the score is weighted.

### Affected Components

1. `internal/config/config.go` — load profile
2. `internal/astro/targets.go` — filter/annotate targets based on equipment capabilities
3. `internal/scoring/score.go` — adjust weights based on rig type
4. `cmd/notify/main.go` — thread profile through to scoring and targets
5. `internal/discord/webhook.go` — display equipment-aware settings
6. `docker-compose.yml` — mount profile volume

## Design

### Profile File: `profile.json`

A single JSON file mounted into the container. Structure:

```json
{
  "rigs": [
    {
      "name": "Nikon D3500 (Untracked)",
      "type": "untracked-dslr",
      "camera": "Nikon D3500",
      "tracked": false,
      "lenses": [
        {"focal_length_mm": 18, "aperture": 3.5, "name": "18-55mm kit (wide end)"},
        {"focal_length_mm": 55, "aperture": 5.6, "name": "18-55mm kit (tele end)"},
        {"focal_length_mm": 70, "aperture": 4.5, "name": "70-300mm (wide end)"},
        {"focal_length_mm": 300, "aperture": 6.3, "name": "70-300mm (tele end)"}
      ],
      "max_exposure_sec": 25,
      "sensor_crop_factor": 1.5
    },
    {
      "name": "Seestar S30 Pro",
      "type": "smart-telescope",
      "tracked": true,
      "aperture_mm": 50,
      "focal_length_mm": 250,
      "sensor_crop_factor": 1.0,
      "integrated_stacking": true,
      "max_exposure_sec": 600,
      "fov_degrees": 1.3
    }
  ],
  "site": {
    "bortle_class": 4,
    "horizon_obstructions": ["south-low-trees"]
  }
}
```

### How Profile Drives Behavior

**Target Selection:**
- Each target in the catalog gets `min_focal_mm`, `max_focal_mm`, `requires_tracking`, and `angular_size_deg` fields
- `SuggestTargets()` accepts the profile and filters to targets achievable by at least one rig
- Settings text is generated dynamically from the matched rig's capabilities (e.g., "Seestar: 10min stack" or "D3500 @ 55mm: 8s, ISO 3200, f/5.6")
- Rule of 500 computes max shutter for untracked rigs: `500 / (focal_length * crop_factor)`

**Scoring Weights:**
- If any rig is tracked + focal_length > 100mm: seeing weight increases from 0% to 10% (taken from wind and precip)
- If all rigs are untracked: current weights preserved
- Bortle class informs a "light pollution impact" note in recommendations

**Recommendations:**
- Text adapts: "wide-field imaging" vs. "deep-sky stacking" vs. "both wide-field and deep-sky" depending on rig mix
- Dew warnings reference specific equipment (lens cloth for DSLR, Seestar's built-in heater)

### New Target Catalog Entries (Seestar-class)

These require tracking and longer focal lengths:

| Target | Type | Months | FL Range | Requires Dark | Angular Size |
|--------|------|--------|----------|---------------|--------------|
| Andromeda Galaxy (M31) | galaxy | 8-12 | 50-300mm | Yes | 3.0 deg |
| Orion Nebula (M42) detail | nebula | 11-3 | 150-500mm | No | 1.0 deg |
| Dumbbell Nebula (M27) | nebula | 6-10 | 150-500mm | Yes | 0.13 deg |
| Ring Nebula (M57) | nebula | 6-10 | 200-500mm | Yes | 0.04 deg |
| Whirlpool Galaxy (M51) | galaxy | 3-6 | 200-500mm | Yes | 0.19 deg |
| Lagoon Nebula (M8) | nebula | 6-8 | 100-300mm | Yes | 1.5 deg |
| Trifid Nebula (M20) | nebula | 6-8 | 150-300mm | Yes | 0.5 deg |
| Eagle Nebula (M16) | nebula | 6-8 | 150-300mm | Yes | 0.6 deg |
| Hercules Cluster (M13) | cluster | 5-9 | 150-500mm | No | 0.33 deg |
| Wild Duck Cluster (M11) | cluster | 6-9 | 100-300mm | No | 0.23 deg |
| North America Nebula (NGC 7000) | nebula | 6-10 | 50-200mm | Yes | 2.0 deg |
| Bode's Galaxy (M81/M82) | galaxy | 11-5 | 200-500mm | Yes | 0.4 deg |

### Implementation Plan

#### 1. Add profile data structures (`internal/config/profile.go` — new file)

```go
type Profile struct {
    Rigs []Rig `json:"rigs"`
    Site Site  `json:"site"`
}

type Rig struct {
    Name               string  `json:"name"`
    Type               string  `json:"type"`
    Camera             string  `json:"camera,omitempty"`
    Tracked            bool    `json:"tracked"`
    Lenses             []Lens  `json:"lenses,omitempty"`
    ApertureMM         float64 `json:"aperture_mm,omitempty"`
    FocalLengthMM      int     `json:"focal_length_mm,omitempty"`
    SensorCropFactor   float64 `json:"sensor_crop_factor"`
    IntegratedStacking bool    `json:"integrated_stacking,omitempty"`
    MaxExposureSec     int     `json:"max_exposure_sec"`
    FOVDegrees         float64 `json:"fov_degrees,omitempty"`
}

type Lens struct {
    FocalLengthMM int     `json:"focal_length_mm"`
    Aperture      float64 `json:"aperture"`
    Name          string  `json:"name"`
}

type Site struct {
    BortleClass         int      `json:"bortle_class"`
    HorizonObstructions []string `json:"horizon_obstructions,omitempty"`
}
```

Loader reads from `PROFILE_PATH` env var (default `./profile.json`). If the file does not exist, returns a hardcoded default matching the current behavior (Nikon D3500 untracked with two kit lenses).

#### 2. Expand the target catalog (`internal/astro/targets.go`)

- Add `MinFocalMM`, `MaxFocalMM`, `RequiresTracking`, `AngularSizeDeg` fields to the `Target` struct
- Add the Seestar-class deep-sky targets from the table above
- Backfill existing targets with their focal length constraints (all have min=18, max=300, no tracking required)
- Remove hardcoded `Lens` and `Settings` strings from the catalog entries (replaced by dynamic generation)

#### 3. New `TargetSuggestion` type and updated `SuggestTargets()` signature

```go
type TargetSuggestion struct {
    Target   Target
    Rig      string   // which rig to use
    Settings string   // computed settings string
}

func SuggestTargets(month time.Month, moonIllumination float64, score float64, profile *config.Profile) []TargetSuggestion
```

Logic:
- For each catalog target, check if any rig can reach it (focal length in range, tracking if required, FOV covers the angular size)
- For untracked rigs: compute max exposure via rule of 500, format ISO/aperture settings
- For smart telescopes: format as "10-30min integrated stack"
- Return up to 5 suggestions, prioritizing by suitability score (matching FOV, conditions)

#### 4. Adjust scoring weights (`internal/scoring/score.go`)

Modify `Generate()` to accept the profile:

```go
func Generate(wx *weather.NighttimeWeather, astroWx *weather.AstroConditions, moon *astro.MoonInfo, planets []astro.PlanetInfo, profile *config.Profile) *Report
```

Weight selection:
- If `profile.HasTrackedLongFL()` (any rig tracked with FL > 100mm):
  - Cloud: 35%, Transparency: 20%, Seeing: 10%, Moon: 15%, Humidity: 10%, Wind: 5%, Precip: 5%
- Otherwise (current behavior):
  - Cloud: 40%, Transparency: 25%, Moon: 15%, Humidity: 10%, Wind: 5%, Precip: 5%

Update `SeeingNote` dynamically based on whether seeing is scored.

#### 5. Update recommendations (`internal/scoring/score.go`)

`generateRecommendation()` gets access to the profile (via the Report struct or direct param):
- Replace "wide-field imaging" with equipment-appropriate text
- Reference specific equipment in dew/wind warnings
- When Seestar is present and conditions are excellent, mention deep-sky stacking potential

#### 6. Thread profile through `cmd/notify/main.go`

- Load profile at startup (after config)
- Pass to `scoring.Generate()` and `astro.SuggestTargets()`
- Both the cron webhook and the `/forecast` slash command use it

#### 7. Update Discord formatting (`internal/discord/webhook.go`)

- `formatTargets()` now receives `[]astro.TargetSuggestion` and shows per-rig settings
- Format example:
  ```
  - **Milky Way Core** : Galactic center rising in the south
    D3500 @ 18mm: 15s, ISO 3200, f/3.5
  - **Lagoon Nebula (M8)** : Bright emission nebula in Sagittarius
    Seestar S30 Pro: 15-20min integrated stack
  ```

#### 8. Docker compose + example files

- Add volume mount: `./profile.json:/app/profile.json:ro`
- Create `profile.example.json` with a reasonable default setup
- Add `profile.json` to `.gitignore`

## Risks and Considerations

- **Backward compatibility**: If no `profile.json` exists, the loader returns a default profile matching current behavior. Zero breaking changes for an existing deployment that doesn't add the file.
- **Seestar FOV**: At 250mm f/5 with a ~1.3 degree FOV, M31 (3+ degrees) overflows the frame. Targets larger than the FOV should still be suggested (partial framing is valid for the Seestar's mosaic mode) but with a note.
- **Scoring stability**: Adding seeing weight when a tracked rig is present means the same weather gives a slightly different score. Acceptable since the user's observing goals have expanded.
- **Target count**: With two rigs, more targets match. The 5-target cap stays, but selection should prioritize variety (mix of DSLR and Seestar targets).
- **Future lens additions**: User just adds a `{"focal_length_mm": 35, "aperture": 1.8, "name": "35mm f/1.8 prime"}` entry to the lenses array and targets matching that FL become available on next run.

## Summary Table

| # | Component | Change | Priority |
|---|-----------|--------|----------|
| 1 | `internal/config/profile.go` | New file: profile types + loader | High |
| 2 | `internal/astro/targets.go` | Expand Target struct, add deep-sky catalog, profile-aware filtering | High |
| 3 | `internal/scoring/score.go` | Accept profile, adjust weights for tracked rigs | High |
| 4 | `cmd/notify/main.go` | Load profile, thread through calls | High |
| 5 | `internal/discord/webhook.go` | Dynamic per-rig settings display | Medium |
| 6 | `docker-compose.yml` | Volume mount for profile | Low |
| 7 | `profile.example.json` | Example config shipped with repo | Low |
| 8 | `.gitignore` | Add `profile.json` | Low |

## Code Comments Plan

- `profile.go`: Comment explaining the fallback behavior when no profile file exists
- `targets.go`: Comment on the `RequiresTracking`/`MinFocalMM` filtering logic and rule-of-500 computation
- `score.go`: Comment explaining why weights shift when tracked equipment is present
- `SuggestTargets()`: Comment explaining how targets are matched to rigs and settings are computed

---

Planning document complete. Please review. I'll proceed with implementation after your approval.
