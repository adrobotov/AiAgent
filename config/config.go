package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds the application configuration.
type Config struct {
	LLM LLMConfig `yaml:"llm"`
	Agent AgentConfig `yaml:"agent"`
}

// LLMConfig holds LLM provider settings.
type LLMConfig struct {
	BaseURL     string `yaml:"base_url"`
	Model       string `yaml:"model"`
	AuthKey     string `yaml:"auth_key"`     // Authorization key for GigaChat OAuth
	OAuthScope  string `yaml:"oauth_scope"`  // OAuth scope for GigaChat
	OAuthRqUID  string `yaml:"oauth_rquid"`  // Fixed RqUID for OAuth requests
	Temperature float64 `yaml:"temperature"`
	MaxTokens   int     `yaml:"max_tokens"`
}

// ProviderURLs returns the default API URL for GigaChat.
func ProviderURLs() map[string]string {
	return map[string]string{
		"gigachat": "https://api.giga.chat/v1",
	}
}

// OAuthURLs returns the OAuth endpoints for GigaChat.
func OAuthURLs() map[string]string {
	return map[string]string{
		"gigachat": "https://ngw.devices.sberbank.ru:9443/api/v2/oauth",
	}
}

// AgentConfig holds agent-specific settings.
type AgentConfig struct {
	SystemPrompt string `yaml:"system_prompt"`
	MaxTurns     int    `yaml:"max_turns"`
}

// DefaultConfig returns a configuration with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		LLM: LLMConfig{
			BaseURL:     ProviderURLs()["gigachat"],
			Model:       "GigaChat-2-Max",
			AuthKey:     "",
			OAuthScope:  "GIGACHAT_API_PERS",
			Temperature: 0.7,
			MaxTokens:   4096,
		},
		Agent: AgentConfig{
			SystemPrompt: "You are a helpful AI assistant.",
			MaxTurns:     50,
		},
	}
}

// Load reads configuration from a YAML file.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// ConfigDir returns the default directory for config files.
func ConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".agent")
}

// ConfigPath returns the full path to the config YAML file.
func ConfigPath() string {
	return filepath.Join(ConfigDir(), "config.yaml")
}
