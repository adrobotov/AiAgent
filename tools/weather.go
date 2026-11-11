package tools

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	weatherTimeout  = 10 * time.Second
	weatherCacheTTL = 10 * time.Minute
	weatherAPIBase  = "http://api.weatherapi.com/v1"
)

// WeatherTool provides weather information via WeatherAPI.com.
type WeatherTool struct {
	// APIKey is the WeatherAPI.com key.
	// If empty, mock data is returned.
	APIKey string

	mu     sync.RWMutex
	cache  map[string]weatherCacheEntry
	client *http.Client
}

type weatherCacheEntry struct {
	data      string
	expiresAt time.Time
}

// NewWeatherTool creates a new weather tool.
func NewWeatherTool(apiKey string) *WeatherTool {
	return &WeatherTool{
		APIKey: apiKey,
		cache:  make(map[string]weatherCacheEntry),
		client: &http.Client{
			Timeout: weatherTimeout,
			Transport: &http.Transport{
				DisableKeepAlives: true,
			},
		},
	}
}

// Name returns the tool name.
func (t *WeatherTool) Name() string { return "weather" }

// Description returns the tool description.
func (t *WeatherTool) Description() string {
	return "Returns current weather for a location using WeatherAPI.com."
}

// Parameters returns the tool parameters schema.
func (t *WeatherTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"location": map[string]interface{}{
				"type":        "string",
				"description": "City or location name (e.g. 'Moscow', 'London').",
			},
		},
		"required": []string{"location"},
	}
}

// Execute executes the weather query.
func (t *WeatherTool) Execute(argsJSON string) (string, error) {
	var args struct {
		Location string `json:"location"`
	}
	if err := parseArgs(argsJSON, &args); err != nil {
		return "", err
	}

	location := strings.TrimSpace(args.Location)
	if location == "" {
		return "", fmt.Errorf("location is required")
	}

	key := strings.ToLower(location)

	// Check cache
	t.mu.RLock()
	if entry, ok := t.cache[key]; ok && time.Now().Before(entry.expiresAt) {
		t.mu.RUnlock()
		return entry.data, nil
	}
	t.mu.RUnlock()

	// Fetch from API
	data, err := t.fetchWeather(location)
	if err != nil {
		return mockWeather(location, err), nil
	}

	// Cache the result
	t.mu.Lock()
	t.cache[key] = weatherCacheEntry{
		data:      data,
		expiresAt: time.Now().Add(weatherCacheTTL),
	}
	t.mu.Unlock()

	return data, nil
}

// fetchWeather queries WeatherAPI.com for current weather.
func (t *WeatherTool) fetchWeather(location string) (string, error) {
	if t.APIKey == "" {
		return "", fmt.Errorf("weather API key not configured")
	}

	encodedLocation := url.QueryEscape(location)
	apiURL := fmt.Sprintf("%s/current.json?key=%s&q=%s&lang=ru",
		weatherAPIBase, t.APIKey, encodedLocation)

	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return "", err
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("weather API returned status %d", resp.StatusCode)
	}

	var result struct {
		Location struct {
			Name    string `json:"name"`
			Country string `json:"country"`
			TzID    string `json:"tz_id"`
		} `json:"location"`
		Current struct {
			TempC       float64 `json:"temp_c"`
			WeatherDesc []struct {
				Value string `json:"text"`
			} `json:"weather_desc"`
			WindKph  float64 `json:"wind_kph"`
			WindDir  string  `json:"wind_dir"`
			Humidity int     `json:"humidity"`
			VisKm    float64 `json:"vis_km"`
		} `json:"current"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	country := result.Location.Country
	if country != "" {
		country = " (" + country + ")"
	}

	desc := "Неизвестно"
	if len(result.Current.WeatherDesc) > 0 {
		desc = result.Current.WeatherDesc[0].Value
	}

	return fmt.Sprintf(
		"Погода в %s%s: %.1f°C, %s, ветер %.1f км/ч (%s), влажность %d%%, видимость %.1f км",
		result.Location.Name, country,
		result.Current.TempC, desc,
		result.Current.WindKph, result.Current.WindDir,
		result.Current.Humidity, result.Current.VisKm,
	), nil
}

// mockWeather returns mock data when the live API is unavailable.
func mockWeather(location string, cause error) string {
	return fmt.Sprintf(
		"Погода в %s: данные недоступны (попробуйте настроить API ключ в config.yaml: weather_api_key: \"ваш_ключ\"). Ошибка: %v",
		location, cause,
	)
}

// FetchWeather is a convenience function for weather queries.
func FetchWeather(argsJSON string) (string, error) {
	tool := NewWeatherTool("")
	return tool.Execute(argsJSON)
}
