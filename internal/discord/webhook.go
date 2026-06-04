package discord

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ldelossa/astro_weather_notify/internal/astro"
	"github.com/ldelossa/astro_weather_notify/internal/scoring"
)

type WebhookPayload struct {
	Embeds []Embed `json:"embeds"`
}

type Embed struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Color       int     `json:"color"`
	Fields      []Field `json:"fields"`
	Footer      Footer  `json:"footer"`
	Timestamp   string  `json:"timestamp"`
}

type Field struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type Footer struct {
	Text string `json:"text"`
}

// Send formats and sends the astro weather report to Discord.
func Send(webhookURL string, report *scoring.Report, locationName string) error {
	embed := BuildEmbed(report, locationName)

	payload := WebhookPayload{
		Embeds: []Embed{embed},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook payload: %w", err)
	}

	resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to send webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func BuildEmbed(report *scoring.Report, locationName string) Embed {
	now := time.Now()

	color := verdictColor(report.Score)

	fields := []Field{
		{
			Name:   "Score",
			Value:  fmt.Sprintf("%.1f / 10", report.Score),
			Inline: true,
		},
		{
			Name:   "Verdict",
			Value:  report.Verdict,
			Inline: true,
		},
		{
			Name:  "Sky Conditions (9PM - 4AM)",
			Value: formatSkyConditions(report),
		},
		{
			Name:  "Moon",
			Value: formatMoon(report.Moon, report.MoonImpact),
		},
	}

	if len(report.Planets) > 0 {
		fields = append(fields, Field{
			Name:  "Visible Planets",
			Value: formatPlanets(report.Planets),
		})
	}

	if len(report.Targets) > 0 && report.Score >= 4 {
		fields = append(fields, Field{
			Name:  "Suggested Targets",
			Value: formatTargets(report.Targets),
		})
	}

	if report.Moon != nil && report.Moon.Available {
		twilight := formatTwilight(report.Moon)
		if twilight != "" {
			fields = append(fields, Field{
				Name:  "Darkness Window",
				Value: twilight,
			})
		}
	}

	if report.Aurora != nil && report.Aurora.Visible {
		fields = append(fields, Field{
			Name:  "Aurora Alert",
			Value: formatAurora(report.Aurora),
		})
	}

	fields = append(fields, Field{
		Name:  "Recommendation",
		Value: report.Recommendation,
	})

	return Embed{
		Title:     fmt.Sprintf("Tonight's Astro Weather - %s", locationName),
		Color:     color,
		Fields:    fields,
		Timestamp: now.Format(time.RFC3339),
		Footer: Footer{
			Text: fmt.Sprintf("Data: %s, 7Timer, USNO, VisiblePlanets", report.WeatherSource),
		},
	}
}

func verdictColor(score float64) int {
	switch {
	case score >= 8:
		return 0x00FF00 // green
	case score >= 6:
		return 0x7CFC00 // lawn green
	case score >= 4:
		return 0xFFD700 // gold
	case score >= 2:
		return 0xFF8C00 // dark orange
	default:
		return 0xFF0000 // red
	}
}

func formatSkyConditions(r *scoring.Report) string {
	s := fmt.Sprintf("Cloud Cover: %.0f%% (%s)\n", r.CloudCoverPct, r.CloudDesc)
	s += fmt.Sprintf("  Low: %.0f%% | Mid: %.0f%% | High: %.0f%%", r.CloudCoverLow, r.CloudCoverMid, r.CloudCoverHigh)
	if r.CloudCoverHigh > 20 {
		s += fmt.Sprintf(" (%s)", r.CloudOpacity)
	}
	s += "\n"

	s += fmt.Sprintf("Transparency: %s", r.TransDesc)
	if r.IWV > 0 {
		s += fmt.Sprintf(" (IWV: %.0f kg/m^2", r.IWV)
		if r.AOD > 0 {
			s += fmt.Sprintf(", AOD: %.2f", r.AOD)
		}
		s += ")"
	}
	s += "\n"

	if r.Seeing > 0 {
		s += fmt.Sprintf("Seeing: %d/8 (%s) -- %s\n", r.Seeing, r.SeeingDesc, r.SeeingNote)
	}

	s += fmt.Sprintf("Humidity: %.0f%% | Dew Spread: %.1fC\n", r.Humidity, r.DewPointSpread)
	s += fmt.Sprintf("Dew Risk: %s\n", r.DewRiskNote)
	s += fmt.Sprintf("Wind: %.0f km/h, gusts %.0f km/h\n", r.WindSpeed, r.WindGusts)

	if r.PrecipProb > 0 {
		s += fmt.Sprintf("Precip Probability: %.0f%%\n", r.PrecipProb)
	}

	if r.JetStreamRisk {
		s += fmt.Sprintf("Jet Stream: %.0f m/s overhead -- cirrus may develop\n", r.JetStreamSpeed)
	}

	return s
}

func formatMoon(moon *astro.MoonInfo, impact string) string {
	if moon == nil || !moon.Available {
		return "Data unavailable"
	}

	s := fmt.Sprintf("Phase: %s (%.0f%% illuminated)\n", moon.Phase, moon.Illumination)

	if moon.Moonrise != "" {
		s += fmt.Sprintf("Moonrise: %s\n", moon.Moonrise)
	}
	if moon.Moonset != "" {
		s += fmt.Sprintf("Moonset: %s\n", moon.Moonset)
	}

	s += fmt.Sprintf("Impact: %s", impact)
	return s
}

func formatTargets(targets []astro.Target) string {
	var s string
	for _, t := range targets {
		s += fmt.Sprintf("- **%s** : %s\n", t.Name, t.Description)
		s += fmt.Sprintf("  Lens: %s | %s\n", t.Lens, t.Settings)
	}
	return s
}

func formatPlanets(planets []astro.PlanetInfo) string {
	var s string
	for _, p := range planets {
		s += fmt.Sprintf("- %s (mag %.1f) in %s\n", p.Name, p.Magnitude, p.Constellation)
	}
	return s
}

func formatTwilight(moon *astro.MoonInfo) string {
	var s string
	if moon.Sunset != "" {
		s += fmt.Sprintf("Sunset: %s\n", moon.Sunset)
	}
	if moon.CivilTwilightEnd != "" {
		s += fmt.Sprintf("Civil Twilight Ends: %s\n", moon.CivilTwilightEnd)
	}
	if moon.CivilTwilightBegin != "" {
		s += fmt.Sprintf("Civil Twilight Begins: %s\n", moon.CivilTwilightBegin)
	}
	if moon.Sunrise != "" {
		s += fmt.Sprintf("Sunrise: %s", moon.Sunrise)
	}
	return s
}

func formatAurora(aurora *astro.AuroraForecast) string {
	if aurora == nil {
		return ""
	}
	s := fmt.Sprintf("Current Kp: %.1f | Next 24h Max: %.1f\n", aurora.CurrentKp, aurora.MaxKp24h)
	switch aurora.AlertLevel {
	case "strong":
		s += "STRONG STORM -- aurora very likely visible! Face north with clear horizon. 18mm, 10-15s, ISO 3200."
	case "likely":
		s += "Aurora likely visible on the northern horizon! Face north, wide lens, 10-15s exposures."
	case "possible":
		s += "Aurora possible -- check north horizon for faint glow. May only show in long exposures."
	}
	return s
}
