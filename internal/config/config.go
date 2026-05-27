package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	DiscordWebhookURL string
	DiscordBotToken   string
	DiscordGuildID    string
	Latitude          float64
	Longitude         float64
	Elevation         float64
	Timezone          string
	LocationName      string
	CronSchedule      string
}

func Load() (*Config, error) {
	webhookURL := os.Getenv("DISCORD_WEBHOOK_URL")
	if webhookURL == "" {
		return nil, fmt.Errorf("DISCORD_WEBHOOK_URL is required")
	}

	botToken := os.Getenv("DISCORD_BOT_TOKEN")
	if botToken == "" {
		return nil, fmt.Errorf("DISCORD_BOT_TOKEN is required")
	}

	cfg := &Config{
		DiscordWebhookURL: webhookURL,
		DiscordBotToken:   botToken,
		DiscordGuildID:    envString("DISCORD_GUILD_ID", ""),
		Latitude:          envFloat("LATITUDE", 42.44),
		Longitude:         envFloat("LONGITUDE", -72.80),
		Elevation:         envFloat("ELEVATION", 444),
		Timezone:          envString("TIMEZONE", "America/New_York"),
		LocationName:      envString("LOCATION_NAME", "Goshen, MA"),
		CronSchedule:      envString("CRON_SCHEDULE", "0 16 * * *"),
	}

	return cfg, nil
}

func envString(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envFloat(key string, fallback float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return f
}
