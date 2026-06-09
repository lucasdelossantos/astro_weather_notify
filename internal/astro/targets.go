package astro

import (
	"fmt"
	"math"
	"time"

	"github.com/lucasdelossantos/astro_weather_notify/internal/config"
)

type Target struct {
	Name            string
	Type            string // widefield, nebula, galaxy, cluster, constellation, moon, event
	Constellation   string
	Magnitude       float64
	BestMonths      []time.Month
	RequiresDark    bool
	RequiresTracking bool
	MinFocalMM      int
	MaxFocalMM      int
	AngularSizeDeg  float64
	Description     string
}

type TargetSuggestion struct {
	Target   Target
	Rig      string
	Settings string
}

// SuggestTargets returns all objects suitable for tonight based on month, moon,
// conditions, and the user's equipment profile.
func SuggestTargets(month time.Month, moonIllumination float64, score float64, profile *config.Profile) []TargetSuggestion {
	var suggestions []TargetSuggestion

	for _, t := range catalog {
		if !inSeason(t, month) {
			continue
		}
		if t.RequiresDark && moonIllumination > 40 {
			continue
		}

		rig, settings := matchRig(t, profile)
		if rig == "" {
			continue
		}

		suggestions = append(suggestions, TargetSuggestion{
			Target:   t,
			Rig:      rig,
			Settings: settings,
		})
	}

	return suggestions
}

// matchRig finds the best rig in the profile that can capture the given target.
// Returns empty strings if no rig can reach it.
func matchRig(t Target, profile *config.Profile) (string, string) {
	for _, rig := range profile.Rigs {
		if t.RequiresTracking && !rig.Tracked {
			continue
		}

		if rig.IntegratedStacking {
			// Smart telescope with fixed optics
			if rig.FocalLengthMM >= t.MinFocalMM && rig.FocalLengthMM <= t.MaxFocalMM {
				settings := formatSmartTelescopeSettings(rig, t)
				return rig.Name, settings
			}
			continue
		}

		// DSLR or similar with interchangeable lenses
		for _, lens := range rig.Lenses {
			if lens.FocalLengthMM >= t.MinFocalMM && lens.FocalLengthMM <= t.MaxFocalMM {
				settings := formatDSLRSettings(rig, lens, t)
				return rig.Name, settings
			}
		}
	}
	return "", ""
}

func formatSmartTelescopeSettings(rig config.Rig, t Target) string {
	switch {
	case t.Magnitude < 0:
		return "Single frame, auto-exposure"
	case t.Magnitude < 4:
		return "10-15min integrated stack"
	default:
		return "20-30min integrated stack"
	}
}

// Rule of 500: max exposure = 500 / (focal_length * crop_factor)
func formatDSLRSettings(rig config.Rig, lens config.Lens, t Target) string {
	crop := rig.SensorCropFactor
	if crop == 0 {
		crop = 1.0
	}

	maxExposure := 500.0 / (float64(lens.FocalLengthMM) * crop)
	if rig.MaxExposureSec > 0 && maxExposure > float64(rig.MaxExposureSec) {
		maxExposure = float64(rig.MaxExposureSec)
	}
	maxExposure = math.Floor(maxExposure)

	iso := 3200
	if t.Magnitude < 0 {
		iso = 100
		maxExposure = 1.0 / 250.0
		return fmt.Sprintf("%s: 1/250s, ISO %d, f/%.1f", lens.Name, iso, lens.Aperture)
	}
	if !t.RequiresDark {
		iso = 1600
	}

	if maxExposure >= 1 {
		return fmt.Sprintf("%s: %.0fs, ISO %d, f/%.1f", lens.Name, maxExposure, iso, lens.Aperture)
	}
	return fmt.Sprintf("%s: %.2fs, ISO %d, f/%.1f", lens.Name, maxExposure, iso, lens.Aperture)
}

func inSeason(t Target, month time.Month) bool {
	for _, m := range t.BestMonths {
		if m == month {
			return true
		}
	}
	return false
}

var catalog = []Target{
	// === WIDEFIELD / MILKY WAY ===
	{
		Name: "Milky Way Core", Type: "widefield", Constellation: "Sagittarius",
		Magnitude: 0, BestMonths: []time.Month{6, 7, 8}, RequiresDark: true,
		RequiresTracking: false, MinFocalMM: 10, MaxFocalMM: 35, AngularSizeDeg: 60,
		Description: "Galactic center rising in the south -- the flagship widefield target",
	},
	{
		Name: "Summer Triangle Region", Type: "widefield", Constellation: "Cygnus/Lyra/Aquila",
		Magnitude: 0, BestMonths: []time.Month{6, 7, 8, 9}, RequiresDark: true,
		RequiresTracking: false, MinFocalMM: 10, MaxFocalMM: 35, AngularSizeDeg: 40,
		Description: "Rich Milky Way star fields with dark lanes -- overhead in summer",
	},
	{
		Name: "Cygnus Widefield", Type: "widefield", Constellation: "Cygnus",
		Magnitude: 0, BestMonths: []time.Month{7, 8, 9, 10}, RequiresDark: true,
		RequiresTracking: false, MinFocalMM: 35, MaxFocalMM: 85, AngularSizeDeg: 10,
		Description: "North America Nebula and Cygnus Rift visible in stacked widefields",
	},
	{
		Name: "Winter Milky Way (Orion Arm)", Type: "widefield", Constellation: "Orion/Monoceros",
		Magnitude: 0, BestMonths: []time.Month{12, 1, 2}, RequiresDark: true,
		RequiresTracking: false, MinFocalMM: 10, MaxFocalMM: 35, AngularSizeDeg: 50,
		Description: "Fainter than summer core but rich with bright nebulae and star clusters",
	},

	// === CONSTELLATIONS ===
	{
		Name: "Orion (full constellation)", Type: "constellation", Constellation: "Orion",
		Magnitude: 0, BestMonths: []time.Month{11, 12, 1, 2, 3}, RequiresDark: false,
		RequiresTracking: false, MinFocalMM: 10, MaxFocalMM: 50, AngularSizeDeg: 20,
		Description: "Belt, sword, Betelgeuse -- even short exposures reveal nebulosity in the sword",
	},
	{
		Name: "Scorpius and Rho Ophiuchi", Type: "constellation", Constellation: "Scorpius",
		Magnitude: 0, BestMonths: []time.Month{6, 7, 8}, RequiresDark: true,
		RequiresTracking: false, MinFocalMM: 35, MaxFocalMM: 85, AngularSizeDeg: 8,
		Description: "Colorful region around Antares -- multicolored nebulosity in stacked shots",
	},
	{
		Name: "Cassiopeia and Perseus", Type: "constellation", Constellation: "Cassiopeia",
		Magnitude: 0, BestMonths: []time.Month{9, 10, 11, 12}, RequiresDark: false,
		RequiresTracking: false, MinFocalMM: 10, MaxFocalMM: 85, AngularSizeDeg: 15,
		Description: "Rich star fields in the Milky Way -- Double Cluster visible to the eye",
	},

	// === BRIGHT NEBULAE (widefield captures) ===
	{
		Name: "Orion Nebula Region (M42)", Type: "nebula", Constellation: "Orion",
		Magnitude: 4.0, BestMonths: []time.Month{11, 12, 1, 2, 3}, RequiresDark: false,
		RequiresTracking: false, MinFocalMM: 35, MaxFocalMM: 300, AngularSizeDeg: 1.0,
		Description: "So bright it shows color even in single short frames",
	},
	{
		Name: "Pleiades (M45)", Type: "cluster", Constellation: "Taurus",
		Magnitude: 1.6, BestMonths: []time.Month{10, 11, 12, 1, 2}, RequiresDark: false,
		RequiresTracking: false, MinFocalMM: 50, MaxFocalMM: 300, AngularSizeDeg: 1.8,
		Description: "Blue reflection nebulosity emerges in stacked frames",
	},

	// === DEEP SKY (tracked telescope targets) ===
	{
		Name: "Andromeda Galaxy (M31)", Type: "galaxy", Constellation: "Andromeda",
		Magnitude: 3.4, BestMonths: []time.Month{8, 9, 10, 11, 12}, RequiresDark: true,
		RequiresTracking: true, MinFocalMM: 50, MaxFocalMM: 400, AngularSizeDeg: 3.0,
		Description: "Nearest large galaxy -- spiral arms visible in stacked exposures",
	},
	{
		Name: "Orion Nebula Detail (M42)", Type: "nebula", Constellation: "Orion",
		Magnitude: 4.0, BestMonths: []time.Month{11, 12, 1, 2, 3}, RequiresDark: false,
		RequiresTracking: true, MinFocalMM: 150, MaxFocalMM: 500, AngularSizeDeg: 1.0,
		Description: "Detailed structure in the Trapezium and nebula wings",
	},
	{
		Name: "Dumbbell Nebula (M27)", Type: "nebula", Constellation: "Vulpecula",
		Magnitude: 7.5, BestMonths: []time.Month{6, 7, 8, 9, 10}, RequiresDark: true,
		RequiresTracking: true, MinFocalMM: 150, MaxFocalMM: 500, AngularSizeDeg: 0.13,
		Description: "Bright planetary nebula -- responds well to short stacked exposures",
	},
	{
		Name: "Ring Nebula (M57)", Type: "nebula", Constellation: "Lyra",
		Magnitude: 8.8, BestMonths: []time.Month{6, 7, 8, 9, 10}, RequiresDark: true,
		RequiresTracking: true, MinFocalMM: 200, MaxFocalMM: 600, AngularSizeDeg: 0.04,
		Description: "Classic planetary nebula -- small but bright, good test target",
	},
	{
		Name: "Whirlpool Galaxy (M51)", Type: "galaxy", Constellation: "Canes Venatici",
		Magnitude: 8.4, BestMonths: []time.Month{3, 4, 5, 6}, RequiresDark: true,
		RequiresTracking: true, MinFocalMM: 200, MaxFocalMM: 600, AngularSizeDeg: 0.19,
		Description: "Face-on spiral with companion -- spiral arms emerge in long stacks",
	},
	{
		Name: "Lagoon Nebula (M8)", Type: "nebula", Constellation: "Sagittarius",
		Magnitude: 6.0, BestMonths: []time.Month{6, 7, 8}, RequiresDark: true,
		RequiresTracking: true, MinFocalMM: 100, MaxFocalMM: 400, AngularSizeDeg: 1.5,
		Description: "Large bright emission nebula near the galactic center",
	},
	{
		Name: "Trifid Nebula (M20)", Type: "nebula", Constellation: "Sagittarius",
		Magnitude: 6.3, BestMonths: []time.Month{6, 7, 8}, RequiresDark: true,
		RequiresTracking: true, MinFocalMM: 150, MaxFocalMM: 400, AngularSizeDeg: 0.5,
		Description: "Emission, reflection, and dark nebula combined in one frame",
	},
	{
		Name: "Eagle Nebula (M16)", Type: "nebula", Constellation: "Serpens",
		Magnitude: 6.0, BestMonths: []time.Month{6, 7, 8}, RequiresDark: true,
		RequiresTracking: true, MinFocalMM: 150, MaxFocalMM: 400, AngularSizeDeg: 0.6,
		Description: "Pillars of Creation region -- bright emission nebula",
	},
	{
		Name: "Hercules Cluster (M13)", Type: "cluster", Constellation: "Hercules",
		Magnitude: 5.8, BestMonths: []time.Month{5, 6, 7, 8, 9}, RequiresDark: false,
		RequiresTracking: true, MinFocalMM: 150, MaxFocalMM: 600, AngularSizeDeg: 0.33,
		Description: "Brightest northern globular -- resolves into stars with stacking",
	},
	{
		Name: "Wild Duck Cluster (M11)", Type: "cluster", Constellation: "Scutum",
		Magnitude: 6.3, BestMonths: []time.Month{6, 7, 8, 9}, RequiresDark: false,
		RequiresTracking: true, MinFocalMM: 100, MaxFocalMM: 400, AngularSizeDeg: 0.23,
		Description: "Dense open cluster -- rich star field at moderate focal lengths",
	},
	{
		Name: "North America Nebula (NGC 7000)", Type: "nebula", Constellation: "Cygnus",
		Magnitude: 4.0, BestMonths: []time.Month{6, 7, 8, 9, 10}, RequiresDark: true,
		RequiresTracking: true, MinFocalMM: 50, MaxFocalMM: 200, AngularSizeDeg: 2.0,
		Description: "Large emission nebula -- distinctive shape at short-medium focal lengths",
	},
	{
		Name: "Bode's Galaxy (M81/M82)", Type: "galaxy", Constellation: "Ursa Major",
		Magnitude: 6.9, BestMonths: []time.Month{11, 12, 1, 2, 3, 4, 5}, RequiresDark: true,
		RequiresTracking: true, MinFocalMM: 200, MaxFocalMM: 600, AngularSizeDeg: 0.4,
		Description: "Galaxy pair -- M82's starburst structure visible in long stacks",
	},

	// === MOON ===
	{
		Name: "Lunar Detail", Type: "moon", Constellation: "",
		Magnitude: -12, BestMonths: []time.Month{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}, RequiresDark: false,
		RequiresTracking: false, MinFocalMM: 200, MaxFocalMM: 2000, AngularSizeDeg: 0.5,
		Description: "Craters and maria -- sharpest detail at quarter phases along the terminator",
	},

	// === STAR TRAILS ===
	{
		Name: "Star Trails (Polaris)", Type: "widefield", Constellation: "Ursa Minor",
		Magnitude: 0, BestMonths: []time.Month{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}, RequiresDark: false,
		RequiresTracking: false, MinFocalMM: 10, MaxFocalMM: 35, AngularSizeDeg: 90,
		Description: "Point north, stack 100+ frames for circular trails around the pole",
	},

	// === CONJUNCTIONS / PLANETS ===
	{
		Name: "Planet Conjunctions", Type: "event", Constellation: "",
		Magnitude: 0, BestMonths: []time.Month{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}, RequiresDark: false,
		RequiresTracking: false, MinFocalMM: 10, MaxFocalMM: 200, AngularSizeDeg: 5,
		Description: "When planets group close together -- scenic with landscape foreground",
	},
}
