# Astro Weather Notify

A Discord bot and webhook notification system that forecasts night-sky conditions for astrophotography. Delivers a scored go/no-go verdict with detailed atmospheric analysis, target suggestions, and setup-specific guidance.

## What It Does

Every day at a configured time (default 4PM), sends a Discord embed with tonight's astrophotography forecast including:

- **Scored verdict** (0-10) with cloud opacity estimation, atmospheric transparency, dew risk, and more
- **Cloud thickness analysis** using pressure-level vertical profiles to distinguish thin cirrus from opaque cirrostratus
- **Transparency forecast** from ECMWF integrated water vapor and CAMS aerosol optical depth
- **Moon phase and impact** with rise/set times and twilight windows
- **Visible planets** and suggested imaging targets for the season
- **Aurora alerts** when Kp index indicates northern lights potential
- **Equipment-specific notes** (dew risk warnings for unprotected optics, jet stream cirrus advisories)

Also provides interactive Discord slash commands for on-demand forecasts and weekly overviews.

## Scoring System

The forecast score weights factors by their actual impact on wide-field astrophotography:

| Factor | Weight | What It Measures |
|--------|--------|------------------|
| Cloud Opacity | 40% | Sky blockage with thickness-aware penalty (not just coverage %) |
| Transparency | 25% | Atmospheric clarity from water vapor column + aerosol depth |
| Moon | 15% | Sky brightness from lunar illumination |
| Humidity/Dew | 10% | Condensation risk on optics (dew point spread) |
| Wind | 5% | Tripod stability |
| Precipitation | 5% | Equipment safety |

Cloud thickness is estimated by counting how many pressure levels (300, 250, 200, 150 hPa) show saturation. A single layer means thin cirrus (light penalty); three or more saturated levels means thick cirrostratus (treated as nearly opaque).

## Data Sources

All free, no API keys required:

| Source | Data |
|--------|------|
| Open-Meteo Forecast (GFS) | Cloud cover by layer, visibility, humidity, temperature, wind, precipitation |
| Open-Meteo Pressure Levels (GFS) | Cloud cover and RH at 300/250/200/150 hPa, jet stream wind |
| Open-Meteo ECMWF | Total column integrated water vapor |
| Open-Meteo Air Quality (CAMS) | Aerosol optical depth at 550nm |
| 7Timer | Astronomical seeing and transparency (supplementary) |
| USNO | Moon phase, illumination, rise/set, twilight times |
| VisiblePlanets API | Planets above the horizon tonight |
| NOAA SWPC | Kp index for aurora forecast |
| In-The-Sky.org | Astronomical events calendar |

## Discord Commands

| Command | Description |
|---------|-------------|
| `/forecast` | Tonight's detailed forecast (default location) |
| `/forecast location:"Denver, CO"` | Forecast for a specific location |
| `/week` | 7-night overview with scores |
| `/events` | Astronomical events in the next 7 days |

## Setup

### Prerequisites

- Go 1.23+
- A Discord bot token (for slash commands)
- A Discord webhook URL (for scheduled notifications)

### Configuration

Copy `.env.example` to `.env` and fill in your values:

```
DISCORD_WEBHOOK_URL=https://discord.com/api/webhooks/your/webhook
DISCORD_BOT_TOKEN=your-bot-token
DISCORD_GUILD_ID=your-guild-id         # optional, speeds up command registration
LATITUDE=42.44
LONGITUDE=-72.80
ELEVATION=444
TIMEZONE=America/New_York
LOCATION_NAME=Goshen, MA
CRON_SCHEDULE=0 16 * * *               # 4PM daily
```

### Run Locally

```bash
go build -o astro-notify ./cmd/notify
./astro-notify
```

### Run with Docker

```bash
docker compose up -d
```

The container runs continuously with a cron scheduler inside. It will send the daily webhook at the configured time and respond to slash commands.

## Project Structure

```
cmd/notify/main.go          Entry point, Discord bot, cron scheduler
internal/
  weather/
    openmeteo.go            Open-Meteo forecast + pressure-level + ECMWF + CAMS
    seventimer.go           7Timer astronomical conditions
    weekly.go               7-day nightly summaries
  scoring/
    score.go                Scoring engine with cloud opacity and transparency
  astro/
    moon.go                 Moon phase and rise/set (USNO + fallback)
    planets.go              Visible planets
    events.go               Astronomical events calendar
    aurora.go               Aurora/Kp forecast
    targets.go              Seasonal target suggestions
  discord/
    webhook.go              Discord embed formatting and sending
  geo/
    geocode.go              Location lookup
  config/
    config.go               Environment variable loading
```

## How Cloud Opacity Works

Standard weather forecasts only report cloud cover percentage per layer (low/mid/high). But 100% high cloud cover can mean either:

- **Thin cirrus** (optical depth ~0.1) -- wispy ice crystals you can sometimes shoot through
- **Thick cirrostratus** (optical depth >1.0) -- a solid veil that glazes over the moon and hides all stars

This system estimates which case you're dealing with by examining the vertical profile:

1. Fetches cloud cover at 4 pressure levels spanning 9-14km altitude
2. Counts how many levels show >70% coverage (vertical extent = thickness)
3. Cross-references with relative humidity saturation depth at those levels
4. Uses surface visibility as a ground-truth constraint

The result is an opacity factor (0.25 for thin cirrus up to 0.95 for thick overcast) that scales the scoring penalty for high clouds.
