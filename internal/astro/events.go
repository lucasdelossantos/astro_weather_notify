package astro

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type AstroEvent struct {
	Date        time.Time
	Summary     string
	Description string
	Context     string // plain-language explanation for astrophotography relevance
}

// FetchWeekEvents fetches astronomical events for the next 7 days from In-The-Sky.org.
func FetchWeekEvents() ([]AstroEvent, error) {
	year := time.Now().Year()
	url := fmt.Sprintf("https://in-the-sky.org/newscalyear_ical.php?year=%d&maxdiff=7", year)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch events: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("events feed returned %d", resp.StatusCode)
	}

	return parseIcal(resp.Body)
}

func parseIcal(r io.Reader) ([]AstroEvent, error) {
	now := time.Now().UTC()
	// Include events from the start of today through 7 days out
	startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	weekEnd := startOfToday.Add(8 * 24 * time.Hour)

	// Unfold iCal lines first (continuation lines start with space/tab)
	lines := unfoldIcal(r)

	var events []AstroEvent
	var summary, description string
	var dtstart time.Time
	inEvent := false

	for _, line := range lines {
		switch {
		case line == "BEGIN:VEVENT":
			inEvent = true
			summary = ""
			description = ""
			dtstart = time.Time{}

		case line == "END:VEVENT":
			if inEvent && !dtstart.IsZero() && summary != "" {
				if (dtstart.Equal(startOfToday) || dtstart.After(startOfToday)) && dtstart.Before(weekEnd) {
					events = append(events, AstroEvent{
						Date:        dtstart,
						Summary:     summary,
						Description: description,
						Context:     annotateEvent(summary),
					})
				}
			}
			inEvent = false

		case inEvent && strings.HasPrefix(line, "SUMMARY:"):
			summary = strings.TrimPrefix(line, "SUMMARY:")

		case inEvent && strings.HasPrefix(line, "DESCRIPTION:"):
			description = strings.TrimPrefix(line, "DESCRIPTION:")
			description = strings.ReplaceAll(description, "\\n", " ")
			description = strings.ReplaceAll(description, "\\,", ",")

		case inEvent && strings.HasPrefix(line, "DTSTART"):
			dtstart = parseIcalDate(line)
		}
	}

	return events, nil
}

func unfoldIcal(r io.Reader) []string {
	var lines []string
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := scanner.Text()
		if len(lines) > 0 && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
			lines[len(lines)-1] += strings.TrimLeft(line, " \t")
		} else {
			lines = append(lines, line)
		}
	}

	return lines
}

// annotateEvent adds a plain-language explanation of what the event means for astrophotography.
func annotateEvent(summary string) string {
	s := strings.ToLower(summary)

	switch {
	// Meteor showers
	case strings.Contains(s, "meteor shower"):
		return "Shooting stars visible -- best after midnight, away from the moon"

	// Moon phases
	case strings.Contains(s, "new moon"):
		return "Darkest skies -- best night for deep-sky and Milky Way"
	case strings.Contains(s, "full moon"):
		return "Very bright sky all night -- tough for faint targets"
	case strings.Contains(s, "first quarter"):
		return "Moon sets around midnight -- deep-sky possible in late hours"
	case strings.Contains(s, "last quarter"):
		return "Moon rises around midnight -- shoot deep-sky in early evening"

	// Moon distance
	case strings.Contains(s, "perigee"):
		return "Moon closest to Earth -- appears slightly larger"
	case strings.Contains(s, "apogee"):
		return "Moon farthest from Earth -- appears slightly smaller"

	// Conjunctions and close approaches
	case strings.Contains(s, "conjunction"):
		return "Two objects appear very close together -- photo opportunity"
	case strings.Contains(s, "close approach"):
		return "Objects appear near each other in the sky -- framing opportunity"

	// Oppositions
	case strings.Contains(s, "opposition"):
		if strings.Contains(s, "asteroid") {
			return "Asteroid at its brightest -- visible all night"
		}
		return "Planet at its brightest and closest -- visible all night, prime imaging"

	// Elongation
	case strings.Contains(s, "greatest elongation"):
		return "Planet at max distance from Sun in the sky -- best visibility window"

	// Lunar occultations
	case strings.Contains(s, "lunar occultation"):
		return "Moon passes in front of a star -- brief disappearance event"

	// Messier / deep sky objects well placed
	case strings.Contains(s, "is well placed"):
		return "Object highest in the sky around midnight -- ideal imaging window"

	// Eclipses
	case strings.Contains(s, "lunar eclipse"):
		return "Earth's shadow on the Moon -- dramatic red/orange color"
	case strings.Contains(s, "solar eclipse"):
		return "Moon blocks the Sun -- DO NOT photograph without proper solar filter"

	// Solar conjunction (planet behind Sun)
	case strings.Contains(s, "solar conjunction"):
		return "Planet behind the Sun -- not visible"

	// Solstice / equinox
	case strings.Contains(s, "solstice"):
		return "Longest or shortest night of the year"
	case strings.Contains(s, "equinox"):
		return "Equal day and night -- 12 hours of darkness"

	// Perihelion / aphelion
	case strings.Contains(s, "perihelion"):
		return "Object closest to the Sun in its orbit"
	case strings.Contains(s, "aphelion"):
		return "Object farthest from the Sun in its orbit"

	default:
		return ""
	}
}

func parseIcalDate(line string) time.Time {
	// Formats: DTSTART;VALUE=DATE:20260521 or DTSTART:20260521T120000Z
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return time.Time{}
	}
	val := strings.TrimSpace(parts[1])

	// Try full datetime
	if t, err := time.Parse("20060102T150405Z", val); err == nil {
		return t
	}
	if t, err := time.Parse("20060102T150405", val); err == nil {
		return t
	}
	// Date only
	if t, err := time.Parse("20060102", val); err == nil {
		return t
	}

	return time.Time{}
}
