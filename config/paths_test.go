package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigDirEndsWithAgent(t *testing.T) {
	dir := ConfigDir()
	if !strings.HasSuffix(filepath.ToSlash(dir), ".agent") {
		t.Fatalf("ConfigDir() = %q, want suffix .agent", dir)
	}
	if dir == "." {
		t.Skip("home directory unavailable")
	}
}

func TestConfigPath(t *testing.T) {
	want := filepath.Join(ConfigDir(), "config.yaml")
	if got := ConfigPath(); got != want {
		t.Fatalf("ConfigPath() = %q, want %q", got, want)
	}
}

func TestProviderURLs(t *testing.T) {
	if got := ProviderURLs()["gigachat"]; got != "https://api.giga.chat/v1" {
		t.Fatalf("gigachat URL = %q", got)
	}
}

func TestOAuthURLs(t *testing.T) {
	if got := OAuthURLs()["gigachat"]; got != "https://ngw.devices.sberbank.ru:9443/api/v2/oauth" {
		t.Fatalf("oauth URL = %q", got)
	}
}
