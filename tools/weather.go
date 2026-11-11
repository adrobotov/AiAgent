package tools

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	weatherTimeout   = 10 * time.Second
	weatherCacheTTL  = 10 * time.Minute
	geocodingBaseURL = "https://geocoding-api.open-meteo.com/v1/search"
	forecastBaseURL  = "https://api.open-meteo.com/v1/forecast"
)

// weatherCacheEntry is a cached weather response with expiration.
type weatherCacheEntry struct {
	data    string
	expires time.Time
}

var (
	weatherCache  sync.Map // location (lowercase) -> weatherCacheEntry
	weatherClient = &http.Client{Timeout: weatherTimeout}
)

// FetchWeather returns current weather for a location using the free
// Open-Meteo API (no API key required). Results are cached for 10 minutes.
// If the API is unreachable, falls back to mock data with a note.
func FetchWeather(argsJSON string) (string, error) {
	var args struct {
		Location string `json:"location"`
	}
	if err := parseArgs(argsJSON, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Location) == "" {
		return "", fmt.Errorf("location is required")
	}

	key := strings.ToLower(strings.TrimSpace(args.Location))

	// Check cache
	if e, ok := weatherCache.Load(key); ok {
		entry := e.(weatherCacheEntry)
		if time.Now().Before(entry.expires) {
			return entry.data, nil
		}
		weatherCache.Delete(key)
	}

	// Primary source: Open-Meteo.
	data, err := fetchLiveWeather(key)
	if err != nil {
		// Fallback source: wttr.in (also keyless).
		if data2, err2 := fetchWttrWeather(args.Location); err2 == nil {
			weatherCache.Store(key, weatherCacheEntry{
				data:    data2,
				expires: time.Now().Add(weatherCacheTTL),
			})
			return data2, nil
		}
		// Last resort: mock data so the agent still gets a usable answer.
		return mockWeather(args.Location, err), nil
	}

	weatherCache.Store(key, weatherCacheEntry{
		data:    data,
		expires: time.Now().Add(weatherCacheTTL),
	})
	return data, nil
}

// fetchLiveWeather queries the Open-Meteo geocoding + forecast APIs.
func fetchLiveWeather(location string) (string, error) {
	lat, lon, name, err := geocode(location)
	if err != nil {
		return "", err
	}

	fcURL := fmt.Sprintf(
		"%s?latitude=%f&longitude=%f&current=temperature_2m,relative_humidity_2m,apparent_temperature,precipitation,weather_code,wind_speed_10m,wind_direction_10m&timezone=auto",
		forecastBaseURL,
		lat,
		lon,
	)

	req, err := http.NewRequest(http.MethodGet, fcURL, nil)
	if err != nil {
		return "", err
	}

	resp, err := weatherClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("forecast request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("forecast API status %d", resp.StatusCode)
	}

	var fc struct {
		Current struct {
			Temperature float64 `json:"temperature_2m"`
			WeatherCode int     `json:"weather_code"`
			WindSpeed   float64 `json:"wind_speed_10m"`
		} `json:"current"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&fc); err != nil {
		return "", fmt.Errorf("parse forecast: %w", err)
	}

	return fmt.Sprintf(
		"Погода в %s: %.1f°C, %s, ветер %.1f м/с (источник: Open-Meteo)",
		name, fc.Current.Temperature,
		weatherDescription(fc.Current.WeatherCode),
		fc.Current.WindSpeed,
	), nil
}

// wttrBaseURL is the fallback weather service (keyless).
const wttrBaseURL = "https://wttr.in"

// fetchWttrWeather fetches current weather from wttr.in (JSON format).
func fetchWttrWeather(location string) (string, error) {
	reqURL := fmt.Sprintf("%s/%s?format=j1", wttrBaseURL, url.QueryEscape(location))

	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "curl/8.0")

	resp, err := weatherClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("wttr.in request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("wttr.in status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read wttr.in response: %w", err)
	}

	name, line, err := parseWttr(body)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Погода в %s: %s (источник: wttr.in)", name, line), nil
}

// parseWttr extracts a human-readable weather line from the wttr.in JSON
// (format=j1) response.
func parseWttr(data []byte) (name, line string, err error) {
	var wttr struct {
		NearestArea []struct {
			AreaName []struct {
				Value string `json:"value"`
			} `json:"areaName"`
			Country []struct {
				Value string `json:"value"`
			} `json:"country"`
		} `json:"nearest_area"`
		CurrentCondition []struct {
			TempC       string `json:"temp_C"`
			WindspeedKm string `json:"windspeedKmph"`
			WeatherDesc []struct {
				Value string `json:"value"`
			} `json:"weatherDesc"`
		} `json:"current_condition"`
	}
	if err := json.Unmarshal(data, &wttr); err != nil {
		return "", "", fmt.Errorf("parse wttr.in response: %w", err)
	}
	if len(wttr.CurrentCondition) == 0 {
		return "", "", fmt.Errorf("wttr.in response has no current condition")
	}

	cc := wttr.CurrentCondition[0]
	desc := "unknown"
	if len(cc.WeatherDesc) > 0 {
		desc = strings.TrimSpace(cc.WeatherDesc[0].Value)
	}

	name = "unknown location"
	if len(wttr.NearestArea) > 0 && len(wttr.NearestArea[0].AreaName) > 0 {
		name = wttr.NearestArea[0].AreaName[0].Value
		if len(wttr.NearestArea[0].Country) > 0 {
			name += ", " + wttr.NearestArea[0].Country[0].Value
		}
	}

	wind := cc.WindspeedKm
	if wind == "" {
		wind = "0"
	}

	return name, fmt.Sprintf(
		"%s°C, %s, ветер %s км/ч",
		cc.TempC, desc, wind,
	), nil
}

// geocode resolves a location name to coordinates via Open-Meteo geocoding.
func geocode(location string) (lat, lon float64, name string, err error) {
	gURL := fmt.Sprintf("%s?name=%s&count=1&language=ru&format=json",
		geocodingBaseURL, url.QueryEscape(location))

	req, err := http.NewRequest(http.MethodGet, gURL, nil)
	if err != nil {
		return 0, 0, "", err
	}

	resp, err := weatherClient.Do(req)
	if err != nil {
		return 0, 0, "", fmt.Errorf("geocoding request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return 0, 0, "", fmt.Errorf("geocoding API status %d", resp.StatusCode)
	}

	var geo struct {
		Results []struct {
			Name      string  `json:"name"`
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&geo); err != nil {
		return 0, 0, "", fmt.Errorf("parse geocoding: %w", err)
	}

	if len(geo.Results) == 0 {
		return 0, 0, "", fmt.Errorf("location %q not found", location)
	}

	r := geo.Results[0]
	return r.Latitude, r.Longitude, r.Name, nil
}

// weatherDescription maps WMO weather codes to human-readable descriptions.
func weatherDescription(code int) string {
	switch {
	case code == 0:
		return "Ясно"
	case code >= 1 && code <= 3:
		return "Переменная облачность"
	case code == 45 || code == 48:
		return "Туман"
	case code >= 51 && code <= 57:
		return "Морось"
	case code >= 61 && code <= 67:
		return "Дождь"
	case code >= 71 && code <= 77:
		return "Снег"
	case code >= 80 && code <= 82:
		return "Ливни"
	case code == 85 || code == 86:
		return "Снегопад"
	case code == 95 || code == 96 || code == 99:
		return "Гроза"
	default:
		return fmt.Sprintf("Код погоды %d", code)
	}
}

// mockWeather returns mock data when the live API is unavailable.
func mockWeather(location string, cause error) string {
	return fmt.Sprintf(
		"Погода в %s: +12°C, Переменная облачность (данные недоступны: %v — показаны моковые данные)",
		location, cause,
	)
}
