package astro

import "time"

type Target struct {
	Name          string
	Type          string // widefield, nebula, cluster, constellation, moon, event
	Constellation string
	Magnitude     float64
	BestMonths    []time.Month
	RequiresDark  bool
	Description   string
	Lens          string // which lens to use
	Settings      string // recommended camera settings
}

// SuggestTargets returns objects suitable for tonight based on month, moon, and conditions.
// Targets are tailored for an untracked DSLR on a tripod (Nikon D3500 with 18-55mm and 70-300mm).
func SuggestTargets(month time.Month, moonIllumination float64, score float64) []Target {
	var suggestions []Target

	for _, t := range catalog {
		if !inSeason(t, month) {
			continue
		}
		if t.RequiresDark && moonIllumination > 40 {
			continue
		}
		suggestions = append(suggestions, t)
	}

	if len(suggestions) > 5 {
		suggestions = suggestions[:5]
	}

	return suggestions
}

func inSeason(t Target, month time.Month) bool {
	for _, m := range t.BestMonths {
		if m == month {
			return true
		}
	}
	return false
}

// catalog of targets achievable with an untracked DSLR on tripod from mid-northern latitudes
var catalog = []Target{
	// === WIDEFIELD / MILKY WAY ===
	{
		Name: "Milky Way Core", Type: "widefield", Constellation: "Sagittarius",
		Magnitude: 0, BestMonths: []time.Month{6, 7, 8}, RequiresDark: true,
		Description: "Galactic center rising in the south -- the flagship widefield target",
		Lens: "18mm", Settings: "15-20s, ISO 3200, f/3.5",
	},
	{
		Name: "Summer Triangle Region", Type: "widefield", Constellation: "Cygnus/Lyra/Aquila",
		Magnitude: 0, BestMonths: []time.Month{6, 7, 8, 9}, RequiresDark: true,
		Description: "Rich Milky Way star fields with dark lanes -- overhead in summer",
		Lens: "18mm", Settings: "15-20s, ISO 3200, f/3.5",
	},
	{
		Name: "Cygnus Widefield", Type: "widefield", Constellation: "Cygnus",
		Magnitude: 0, BestMonths: []time.Month{7, 8, 9, 10}, RequiresDark: true,
		Description: "North America Nebula and Cygnus Rift visible in stacked widefields",
		Lens: "55mm", Settings: "8-10s, ISO 3200, f/5.6 -- stack 20+ frames",
	},
	{
		Name: "Winter Milky Way (Orion Arm)", Type: "widefield", Constellation: "Orion/Monoceros",
		Magnitude: 0, BestMonths: []time.Month{12, 1, 2}, RequiresDark: true,
		Description: "Fainter than summer core but rich with bright nebulae and star clusters",
		Lens: "18mm", Settings: "15-20s, ISO 3200, f/3.5",
	},

	// === CONSTELLATIONS ===
	{
		Name: "Orion (full constellation)", Type: "constellation", Constellation: "Orion",
		Magnitude: 0, BestMonths: []time.Month{11, 12, 1, 2, 3}, RequiresDark: false,
		Description: "Belt, sword, Betelgeuse -- even short exposures reveal nebulosity in the sword",
		Lens: "18-35mm", Settings: "15s, ISO 1600-3200, f/3.5",
	},
	{
		Name: "Scorpius and Rho Ophiuchi", Type: "constellation", Constellation: "Scorpius",
		Magnitude: 0, BestMonths: []time.Month{6, 7, 8}, RequiresDark: true,
		Description: "Colorful region around Antares -- multicolored nebulosity in stacked shots",
		Lens: "55mm", Settings: "8-10s, ISO 3200, f/5.6 -- stack many frames",
	},
	{
		Name: "Cassiopeia and Perseus", Type: "constellation", Constellation: "Cassiopeia",
		Magnitude: 0, BestMonths: []time.Month{9, 10, 11, 12}, RequiresDark: false,
		Description: "Rich star fields in the Milky Way -- Double Cluster visible to the eye",
		Lens: "18-55mm", Settings: "15s, ISO 3200, f/3.5",
	},

	// === BRIGHT NEBULAE (widefield captures) ===
	{
		Name: "Orion Nebula Region (M42)", Type: "nebula", Constellation: "Orion",
		Magnitude: 4.0, BestMonths: []time.Month{11, 12, 1, 2, 3}, RequiresDark: false,
		Description: "So bright it shows color even in single 15s frames at 55mm",
		Lens: "55mm", Settings: "10s, ISO 1600, f/5.6 -- stack for detail",
	},
	{
		Name: "Pleiades (M45)", Type: "cluster", Constellation: "Taurus",
		Magnitude: 1.6, BestMonths: []time.Month{10, 11, 12, 1, 2}, RequiresDark: false,
		Description: "Blue reflection nebulosity emerges in stacked frames at 55mm+",
		Lens: "55-70mm", Settings: "8-10s, ISO 3200, f/5.6 -- stack 30+ frames",
	},

	// === MOON ===
	{
		Name: "Lunar Detail", Type: "moon", Constellation: "",
		Magnitude: -12, BestMonths: []time.Month{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}, RequiresDark: false,
		Description: "Craters and maria -- sharpest detail at quarter phases along the terminator",
		Lens: "300mm", Settings: "1/250s, ISO 100-400, f/6.3 -- use 2s timer or remote",
	},

	// === STAR TRAILS ===
	{
		Name: "Star Trails (Polaris)", Type: "widefield", Constellation: "Ursa Minor",
		Magnitude: 0, BestMonths: []time.Month{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}, RequiresDark: false,
		Description: "Point north, stack 100+ frames for circular trails around the pole",
		Lens: "18mm", Settings: "20s per frame, ISO 800, f/3.5 -- continuous shooting 30-60 min",
	},

	// === CONJUNCTIONS / PLANETS ===
	{
		Name: "Planet Conjunctions", Type: "event", Constellation: "",
		Magnitude: 0, BestMonths: []time.Month{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}, RequiresDark: false,
		Description: "When planets group close together -- scenic with landscape foreground",
		Lens: "18-70mm", Settings: "2-5s, ISO 800-1600, f/3.5-5.6",
	},

}
