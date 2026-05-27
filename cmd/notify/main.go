package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/robfig/cron/v3"

	"github.com/ldelossa/astro_weather_notify/internal/astro"
	"github.com/ldelossa/astro_weather_notify/internal/config"
	"github.com/ldelossa/astro_weather_notify/internal/discord"
	"github.com/ldelossa/astro_weather_notify/internal/geo"
	"github.com/ldelossa/astro_weather_notify/internal/scoring"
	"github.com/ldelossa/astro_weather_notify/internal/weather"
)

var cfg *config.Config

func main() {
	var err error
	cfg, err = config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	// Create Discord bot session
	session, err := discordgo.New("Bot " + cfg.DiscordBotToken)
	if err != nil {
		log.Fatalf("failed to create discord session: %v", err)
	}

	session.AddHandler(handleInteraction)
	session.Identify.Intents = discordgo.IntentsGuilds

	if err := session.Open(); err != nil {
		log.Fatalf("failed to open discord connection: %v", err)
	}
	defer session.Close()

	// Register slash commands (guild-scoped for instant availability)
	locationOption := &discordgo.ApplicationCommandOption{
		Type:        discordgo.ApplicationCommandOptionString,
		Name:        "location",
		Description: "City name or place (e.g. 'Denver, CO' or 'Portland, OR')",
		Required:    false,
	}

	commands := []*discordgo.ApplicationCommand{
		{
			Name:        "forecast",
			Description: "Get tonight's astrophotography weather forecast",
			Options:     []*discordgo.ApplicationCommandOption{locationOption},
		},
		{
			Name:        "events",
			Description: "Show astronomical events for the next 7 days",
		},
		{
			Name:        "week",
			Description: "7-day night sky forecast overview with scores",
			Options:     []*discordgo.ApplicationCommandOption{locationOption},
		},
	}

	// Clear old command registrations so option changes take effect
	existingCmds, _ := session.ApplicationCommands(session.State.User.ID, cfg.DiscordGuildID)
	for _, cmd := range existingCmds {
		session.ApplicationCommandDelete(session.State.User.ID, cfg.DiscordGuildID, cmd.ID)
	}

	for _, cmd := range commands {
		registered, err := session.ApplicationCommandCreate(session.State.User.ID, cfg.DiscordGuildID, cmd)
		if err != nil {
			log.Fatalf("failed to register /%s command: %v", cmd.Name, err)
		}
		log.Printf("registered slash command: /%s (guild: %s)", registered.Name, cfg.DiscordGuildID)
	}

	// Start the cron scheduler for daily webhook notifications
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		log.Fatalf("invalid timezone %s: %v", cfg.Timezone, err)
	}

	c := cron.New(cron.WithLocation(loc))
	_, err = c.AddFunc(cfg.CronSchedule, func() {
		log.Println("cron: running scheduled notification")
		if err := sendWebhookNotification(); err != nil {
			log.Printf("cron: notification error: %v", err)
		}
	})
	if err != nil {
		log.Fatalf("failed to add cron schedule: %v", err)
	}
	c.Start()
	log.Printf("scheduled daily notification: %s (%s)", cfg.CronSchedule, cfg.Timezone)

	fmt.Fprintf(os.Stderr, "Bot is running. /forecast is available. Ctrl+C to stop.\n")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Println("shutting down...")
	c.Stop()
}

func handleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	switch i.ApplicationCommandData().Name {
	case "forecast":
		handleForecast(s, i)
	case "events":
		handleEvents(s, i)
	case "week":
		handleWeek(s, i)
	}
}

func handleForecast(s *discordgo.Session, i *discordgo.InteractionCreate) {
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})

	loc := resolveLocation(i)

	report, err := generateReportForLocation(loc)
	if err != nil {
		errMsg := fmt.Sprintf("Error generating forecast: %v", err)
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: &errMsg,
		})
		return
	}

	embed := discord.BuildEmbed(report, loc.name)
	s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{toDiscordGoEmbed(embed)},
	})
}

func handleEvents(s *discordgo.Session, i *discordgo.InteractionCreate) {
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})

	events, err := astro.FetchWeekEvents()
	if err != nil {
		errMsg := fmt.Sprintf("Error fetching events: %v", err)
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: &errMsg,
		})
		return
	}

	embed := buildEventsEmbed(events)
	s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{embed},
	})
}

func handleWeek(s *discordgo.Session, i *discordgo.InteractionCreate) {
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})

	loc := resolveLocation(i)

	nights, err := weather.FetchWeeklyNights(loc.lat, loc.lon, loc.tz)
	if err != nil {
		errMsg := fmt.Sprintf("Error fetching weekly forecast: %v", err)
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: &errMsg,
		})
		return
	}

	embed := buildWeekEmbed(nights, loc.name)
	s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{embed},
	})
}

func buildWeekEmbed(nights []weather.NightSummary, locationName string) *discordgo.MessageEmbed {
	embed := &discordgo.MessageEmbed{
		Title: fmt.Sprintf("7-Night Forecast - %s", locationName),
		Color: 0x1E90FF,
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Scores based on cloud cover, wind, and precipitation only (no seeing data for multi-day)",
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}

	var description string
	for _, n := range nights {
		score := weather.QuickScore(n)
		bar := scoreBar(score)
		dayName := n.Date.Format("Mon Jan 2")

		verdict := ""
		switch {
		case score >= 8:
			verdict = "Excellent"
		case score >= 6:
			verdict = "Good"
		case score >= 4:
			verdict = "Marginal"
		case score >= 2:
			verdict = "Poor"
		default:
			verdict = "No go"
		}

		description += fmt.Sprintf("**%s** : %s %.1f/10 - %s\n", dayName, bar, score, verdict)
		description += fmt.Sprintf("  Cloud: Low %.0f%% | Mid %.0f%% | High %.0f%% | Wind: %.0f km/h | Precip: %.0f%%\n\n",
			n.AvgCloudLow, n.AvgCloudMid, n.AvgCloudHigh, n.AvgWindSpeed, n.MaxPrecipProb)
	}

	embed.Description = description
	return embed
}

func scoreBar(score float64) string {
	filled := int(score)
	empty := 10 - filled
	bar := ""
	for i := 0; i < filled; i++ {
		bar += "█" // full block
	}
	for i := 0; i < empty; i++ {
		bar += "░" // light shade
	}
	return bar
}

func buildEventsEmbed(events []astro.AstroEvent) *discordgo.MessageEmbed {
	embed := &discordgo.MessageEmbed{
		Title: "Astronomical Events - Next 7 Days",
		Color: 0x6A0DAD,
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Data: In-The-Sky.org",
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}

	if len(events) == 0 {
		embed.Description = "No notable astronomical events in the next 7 days."
		return embed
	}

	// Group events by date
	type eventEntry struct {
		summary string
		context string
	}
	type dayEvents struct {
		date   string
		events []eventEntry
	}
	var days []dayEvents
	var currentDay string

	for _, e := range events {
		dateStr := e.Date.Format("Mon Jan 2")
		if dateStr != currentDay {
			days = append(days, dayEvents{date: dateStr})
			currentDay = dateStr
		}
		days[len(days)-1].events = append(days[len(days)-1].events, eventEntry{
			summary: e.Summary,
			context: e.Context,
		})
	}

	// Use embed fields -- one field per day
	for _, day := range days {
		var value string
		for _, ev := range day.events {
			var line string
			if ev.context != "" {
				line = fmt.Sprintf("- **%s** : _%s_\n", ev.summary, ev.context)
			} else {
				line = fmt.Sprintf("- **%s**\n", ev.summary)
			}
			if len(value)+len(line) > 1024 {
				value += "- _...and more_\n"
				break
			}
			value += line
		}
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:  day.date,
			Value: value,
		})
	}

	return embed
}

type locationParams struct {
	name string
	lat  float64
	lon  float64
	elev float64
	tz   string
}

// resolveLocation extracts the optional location parameter from a slash command.
// Falls back to the configured default if not provided.
func resolveLocation(i *discordgo.InteractionCreate) locationParams {
	opts := i.ApplicationCommandData().Options
	for _, opt := range opts {
		if opt.Name == "location" {
			query := opt.StringValue()
			loc, err := geo.Lookup(query)
			if err == nil {
				return locationParams{
					name: loc.Name,
					lat:  loc.Latitude,
					lon:  loc.Longitude,
					elev: loc.Elevation,
					tz:   loc.Timezone,
				}
			}
			// If geocoding fails, fall through to default
			log.Printf("geocoding failed for '%s': %v", query, err)
		}
	}
	return locationParams{
		name: cfg.LocationName,
		lat:  cfg.Latitude,
		lon:  cfg.Longitude,
		elev: cfg.Elevation,
		tz:   cfg.Timezone,
	}
}

func generateReportForLocation(loc locationParams) (*scoring.Report, error) {
	wx, err := weather.FetchNighttimeWeather(loc.lat, loc.lon, loc.tz)
	if err != nil {
		return nil, fmt.Errorf("open-meteo: %w", err)
	}

	// Fetch pressure-level data for cloud thickness estimation
	pressureData, err := weather.FetchPressureLevelData(loc.lat, loc.lon, loc.tz)
	if err != nil {
		log.Printf("pressure-level data unavailable: %v", err)
	} else {
		if err := weather.ExtractNighttimePressureLevel(wx, pressureData, loc.tz); err != nil {
			log.Printf("pressure-level extraction failed: %v", err)
		}
	}

	// Fetch ECMWF integrated water vapor for transparency estimation
	ecmwfData, err := weather.FetchECMWFData(loc.lat, loc.lon, loc.tz)
	if err != nil {
		log.Printf("ECMWF data unavailable: %v", err)
	} else {
		if err := weather.ExtractNighttimeIWV(wx, ecmwfData, loc.tz); err != nil {
			log.Printf("ECMWF IWV extraction failed: %v", err)
		}
	}

	// Fetch aerosol optical depth from CAMS
	aqData, err := weather.FetchAirQuality(loc.lat, loc.lon, loc.tz)
	if err != nil {
		log.Printf("air quality data unavailable: %v", err)
	} else {
		if err := weather.ExtractNighttimeAOD(wx, aqData, loc.tz); err != nil {
			log.Printf("AOD extraction failed: %v", err)
		}
	}

	astroWx, err := weather.FetchAstroConditions(loc.lat, loc.lon)
	if err != nil {
		astroWx = &weather.AstroConditions{Available: false}
	}

	moon, err := astro.FetchMoonInfo(loc.lat, loc.lon, loc.tz)
	if err != nil {
		moon = &astro.MoonInfo{Available: false}
	}

	planets, err := astro.FetchVisiblePlanets(loc.lat, loc.lon, loc.elev, loc.tz)
	if err != nil {
		planets = nil
	}

	aurora, err := astro.FetchAuroraForecast()
	if err != nil {
		aurora = nil
	}

	report := scoring.Generate(wx, astroWx, moon, planets)
	report.Aurora = aurora
	return report, nil
}

func generateReport() (*scoring.Report, error) {
	loc := locationParams{
		name: cfg.LocationName,
		lat:  cfg.Latitude,
		lon:  cfg.Longitude,
		elev: cfg.Elevation,
		tz:   cfg.Timezone,
	}
	return generateReportForLocation(loc)
}

func sendWebhookNotification() error {
	report, err := generateReport()
	if err != nil {
		return err
	}
	return discord.Send(cfg.DiscordWebhookURL, report, cfg.LocationName)
}

func toDiscordGoEmbed(e discord.Embed) *discordgo.MessageEmbed {
	embed := &discordgo.MessageEmbed{
		Title:     e.Title,
		Color:     e.Color,
		Timestamp: e.Timestamp,
		Footer:    &discordgo.MessageEmbedFooter{Text: e.Footer.Text},
	}

	for _, f := range e.Fields {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   f.Name,
			Value:  f.Value,
			Inline: f.Inline,
		})
	}

	return embed
}
