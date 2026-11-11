package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds the application configuration.
type Config struct {
	LLM    LLMConfig    `yaml:"llm"`
	Agent  AgentConfig  `yaml:"agent"`
	Tools  ToolsConfig  `yaml:"tools"`
	Server ServerConfig `yaml:"server"`
}

// ServerConfig holds HTTP API server settings.
type ServerConfig struct {
	Port int `yaml:"port"`
}

// ToolsConfig holds tool sandbox and safety settings.
type ToolsConfig struct {
	// Root restricts the filesystem tool to this directory.
	// If empty, the current working directory is used.
	Root string `yaml:"root"`

	// AllowedCommands is the whitelist for the process tool.
	// If empty, no external commands can be executed.
	AllowedCommands []string `yaml:"allowed_commands"`

	// Confirm controls confirmation of dangerous operations:
	//   ask    - prompt the user before dangerous ops (default)
	//   always - always allow dangerous ops without asking
	//   never  - always deny dangerous ops
	Confirm string `yaml:"confirm"`

	// AutoConfirm enables automatic confirmation for all tool operations.
	// When true, dangerous operations (write, delete, execute) are allowed
	// without user interaction. Useful for autonomous agent mode.
	AutoConfirm bool `yaml:"auto_confirm"`

	// MaxResultSize limits the size of a tool result fed back to the LLM
	// (in characters). Oversized results are truncated.
	MaxResultSize int `yaml:"max_result_size"`

	// Plugins is a list of external tool plugin commands (subprocess protocol).
	Plugins []string `yaml:"plugins"`

	// WeatherAPIKey is the API key for WeatherAPI.com.
	WeatherAPIKey string `yaml:"weather_api_key"`
}

// LLMConfig holds LLM provider settings.
type LLMConfig struct {
	BaseURL     string  `yaml:"base_url"`
	Model       string  `yaml:"model"`
	AuthKey     string  `yaml:"auth_key"`    // Authorization key for GigaChat OAuth
	OAuthScope  string  `yaml:"oauth_scope"` // OAuth scope for GigaChat
	OAuthRqUID  string  `yaml:"oauth_rquid"` // Fixed RqUID for OAuth requests
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
	SystemPrompt       string `yaml:"system_prompt"`
	MaxTurns           int    `yaml:"max_turns"`            // Max tool-calling iterations per request
	MaxHistoryMessages int    `yaml:"max_history_messages"` // Max messages kept in the session context
	ContextTokens      int    `yaml:"context_tokens"`       // Approx. token budget for session history
	Summarize          bool   `yaml:"summarize"`            // LLM-summarize old messages when over budget
	Memory             bool   `yaml:"memory"`               // Enable long-term memory (embeddings)
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
			SystemPrompt:       "You are a helpful AI assistant.",
			MaxTurns:           50,
			MaxHistoryMessages: 20,
			ContextTokens:      8000,
			Summarize:          false,
			Memory:             false,
		},
		Tools: ToolsConfig{
			Root: "",
			AllowedCommands: []string{
				"go", "git", "ls", "dir", "cat", "type", "echo", "pwd",
				"python", "node", "npm", "grep", "findstr",
			},
			Confirm:       "ask",
			MaxResultSize: 20000,
		},
		Server: ServerConfig{
			Port: 8080,
		},
	}
}

// Load reads configuration from a YAML file.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			applyEnvOverrides(cfg)
			return cfg, nil
		}
		return nil, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	applyEnvOverrides(cfg)
	return cfg, nil
}

// applyEnvOverrides overrides config values with environment variables.
// Environment variables take precedence over config.yaml values,
// which allows keeping secrets (auth key) out of config files.
func applyEnvOverrides(cfg *Config) {
	// LLM settings
	if v := os.Getenv("AGENT_AUTH_KEY"); v != "" {
		cfg.LLM.AuthKey = v
	}
	if v := os.Getenv("AGENT_BASE_URL"); v != "" {
		cfg.LLM.BaseURL = v
	}
	if v := os.Getenv("AGENT_MODEL"); v != "" {
		cfg.LLM.Model = v
	}
	if v := os.Getenv("AGENT_OAUTH_SCOPE"); v != "" {
		cfg.LLM.OAuthScope = v
	}
	if v := os.Getenv("AGENT_OAUTH_RQUID"); v != "" {
		cfg.LLM.OAuthRqUID = v
	}
	if v := os.Getenv("AGENT_TEMPERATURE"); v != "" {
		if t, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.LLM.Temperature = t
		}
	}
	if v := os.Getenv("AGENT_MAX_TOKENS"); v != "" {
		if t, err := strconv.Atoi(v); err == nil && t > 0 {
			cfg.LLM.MaxTokens = t
		}
	}

	// Agent settings
	if v := os.Getenv("AGENT_SYSTEM_PROMPT"); v != "" {
		cfg.Agent.SystemPrompt = v
	}
	if v := os.Getenv("AGENT_MAX_TURNS"); v != "" {
		if t, err := strconv.Atoi(v); err == nil && t > 0 {
			cfg.Agent.MaxTurns = t
		}
	}
	if v := os.Getenv("AGENT_MAX_HISTORY"); v != "" {
		if t, err := strconv.Atoi(v); err == nil && t > 0 {
			cfg.Agent.MaxHistoryMessages = t
		}
	}

	// Tools settings
	if v := os.Getenv("AGENT_TOOLS_ROOT"); v != "" {
		cfg.Tools.Root = v
	}
	if v := os.Getenv("AGENT_TOOLS_CONFIRM"); v != "" {
		cfg.Tools.Confirm = v
	}
	if v := os.Getenv("AGENT_TOOLS_MAX_RESULT"); v != "" {
		if t, err := strconv.Atoi(v); err == nil && t > 0 {
			cfg.Tools.MaxResultSize = t
		}
	}
	if v := os.Getenv("AGENT_TOOLS_AUTO_CONFIRM"); v != "" {
		cfg.Tools.AutoConfirm = v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
	}
	if v := os.Getenv("AGENT_WEATHER_API_KEY"); v != "" {
		cfg.Tools.WeatherAPIKey = v
	}

	// Context settings
	if v := os.Getenv("AGENT_CONTEXT_TOKENS"); v != "" {
		if t, err := strconv.Atoi(v); err == nil && t > 0 {
			cfg.Agent.ContextTokens = t
		}
	}
	if v := os.Getenv("AGENT_SUMMARIZE"); v != "" {
		cfg.Agent.Summarize = v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
	}
	if v := os.Getenv("AGENT_MEMORY"); v != "" {
		cfg.Agent.Memory = v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
	}
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
