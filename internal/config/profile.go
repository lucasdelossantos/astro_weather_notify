package config

import (
	"encoding/json"
	"fmt"
	"os"
)

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

// HasTrackedLongFL returns true if any rig is tracked with focal length > 100mm.
func (p *Profile) HasTrackedLongFL() bool {
	for _, r := range p.Rigs {
		if !r.Tracked {
			continue
		}
		if r.FocalLengthMM > 100 {
			return true
		}
		for _, l := range r.Lenses {
			if l.FocalLengthMM > 100 {
				return true
			}
		}
	}
	return false
}

// MaxFocalLength returns the longest focal length available across all rigs.
func (p *Profile) MaxFocalLength() int {
	max := 0
	for _, r := range p.Rigs {
		if r.FocalLengthMM > max {
			max = r.FocalLengthMM
		}
		for _, l := range r.Lenses {
			if l.FocalLengthMM > max {
				max = l.FocalLengthMM
			}
		}
	}
	return max
}

// MinFocalLength returns the shortest focal length available across all rigs.
func (p *Profile) MinFocalLength() int {
	min := 999999
	for _, r := range p.Rigs {
		if r.FocalLengthMM > 0 && r.FocalLengthMM < min {
			min = r.FocalLengthMM
		}
		for _, l := range r.Lenses {
			if l.FocalLengthMM < min {
				min = l.FocalLengthMM
			}
		}
	}
	if min == 999999 {
		return 0
	}
	return min
}

// HasTracking returns true if any rig is tracked.
func (p *Profile) HasTracking() bool {
	for _, r := range p.Rigs {
		if r.Tracked {
			return true
		}
	}
	return false
}

// LoadProfile reads the equipment profile from disk. Falls back to a default
// profile matching the original hardcoded behavior if the file does not exist.
func LoadProfile() *Profile {
	path := envString("PROFILE_PATH", "./profile.json")

	data, err := os.ReadFile(path)
	if err != nil {
		return defaultProfile()
	}

	var p Profile
	if err := json.Unmarshal(data, &p); err != nil {
		fmt.Fprintf(os.Stderr, "warning: invalid profile.json (%v), using defaults\n", err)
		return defaultProfile()
	}

	return &p
}

func defaultProfile() *Profile {
	return &Profile{
		Rigs: []Rig{
			{
				Name:             "Nikon D3500 (Untracked)",
				Type:             "untracked-dslr",
				Camera:           "Nikon D3500",
				Tracked:          false,
				SensorCropFactor: 1.5,
				MaxExposureSec:   25,
				Lenses: []Lens{
					{FocalLengthMM: 18, Aperture: 3.5, Name: "18-55mm kit (wide end)"},
					{FocalLengthMM: 55, Aperture: 5.6, Name: "18-55mm kit (tele end)"},
					{FocalLengthMM: 70, Aperture: 4.5, Name: "70-300mm (wide end)"},
					{FocalLengthMM: 300, Aperture: 6.3, Name: "70-300mm (tele end)"},
				},
			},
		},
		Site: Site{
			BortleClass: 4,
		},
	}
}
