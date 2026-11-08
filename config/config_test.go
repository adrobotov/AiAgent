package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.LLM.BaseURL != "https://api.giga.chat/v1" {
		t.Errorf("unexpected base URL: %s", cfg.LLM.BaseURL)
	}
	if cfg.LLM.Model != "GigaChat-2-Max" {
		t.Errorf("unexpected model: %s", cfg.LLM.Model)
	}
	if cfg.LLM.OAuthScope != "GIGACHAT_API_PERS" {
		t.Errorf("unexpected scope: %s", cfg.LLM.OAuthScope)
	}
	if cfg.Agent.MaxTurns != 50 {
		t.Errorf("unexpected max turns: %d", cfg.Agent.MaxTurns)
	}
	if cfg.Agent.MaxHistoryMessages != 20 {
		t.Errorf("unexpected max history: %d", cfg.Agent.MaxHistoryMessages)
	}
	if cfg.Agent.ContextTokens != 8000 {
		t.Errorf("unexpected context tokens: %d", cfg.Agent.ContextTokens)
	}
	if cfg.Tools.Confirm != "ask" {
		t.Errorf("unexpected confirm mode: %s", cfg.Tools.Confirm)
	}
	if cfg.Tools.MaxResultSize != 20000 {
		t.Errorf("unexpected max result size: %d", cfg.Tools.MaxResultSize)
	}
	if len(cfg.Tools.AllowedCommands) == 0 {
		t.Error("default allowed commands must not be empty")
	}
}

func TestLoadMissingFile(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.LLM.Model != "GigaChat-2-Max" {
		t.Errorf("expected defaults, got model %s", cfg.LLM.Model)
	}
}

func TestLoadYAMLAndEnvPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
llm:
  model: GigaChat-2
  temperature: 0.3
agent:
  max_turns: 5
  max_history_messages: 10
  summarize: true
tools:
  confirm: never
  max_result_size: 500
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.LLM.Model != "GigaChat-2" {
		t.Errorf("model from YAML not applied: %s", cfg.LLM.Model)
	}
	if cfg.LLM.Temperature != 0.3 {
		t.Errorf("temperature from YAML not applied: %v", cfg.LLM.Temperature)
	}
	if cfg.Agent.MaxTurns != 5 {
		t.Errorf("max_turns from YAML not applied: %d", cfg.Agent.MaxTurns)
	}
	if cfg.Agent.Summarize != true {
		t.Error("summarize from YAML not applied")
	}
	if cfg.Tools.Confirm != "never" {
		t.Errorf("confirm from YAML not applied: %s", cfg.Tools.Confirm)
	}

	// Env overrides YAML
	t.Setenv("AGENT_MODEL", "GigaChat-3-Ultra")
	t.Setenv("AGENT_MAX_TURNS", "7")
	t.Setenv("AGENT_TOOLS_CONFIRM", "always")
	t.Setenv("AGENT_CONTEXT_TOKENS", "4000")

	cfg, err = Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.LLM.Model != "GigaChat-3-Ultra" {
		t.Errorf("env model override not applied: %s", cfg.LLM.Model)
	}
	if cfg.Agent.MaxTurns != 7 {
		t.Errorf("env max_turns override not applied: %d", cfg.Agent.MaxTurns)
	}
	if cfg.Tools.Confirm != "always" {
		t.Errorf("env confirm override not applied: %s", cfg.Tools.Confirm)
	}
	if cfg.Agent.ContextTokens != 4000 {
		t.Errorf("env context tokens override not applied: %d", cfg.Agent.ContextTokens)
	}
}

func TestBoolEnvParsing(t *testing.T) {
	for _, val := range []string{"1", "true", "TRUE", "yes"} {
		t.Setenv("AGENT_MEMORY", val)
		cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.Agent.Memory {
			t.Errorf("AGENT_MEMORY=%s should enable memory", val)
		}
	}

	t.Setenv("AGENT_MEMORY", "0")
	cfg, _ := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if cfg.Agent.Memory {
		t.Error("AGENT_MEMORY=0 should not enable memory")
	}
}
