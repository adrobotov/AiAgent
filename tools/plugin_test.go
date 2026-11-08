package tools

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// buildTestPlugin compiles a helper plugin executable implementing the
// plugin protocol and returns its path.
func buildTestPlugin(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	src := filepath.Join(dir, "plugin_main.go")
	code := `package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

type manifest struct {
	Name        string                 ` + "`json:\"name\"`" + `
	Description string                 ` + "`json:\"description\"`" + `
	Parameters  map[string]interface{} ` + "`json:\"parameters\"`" + `
}

func main() {
	argv := os.Args[1:]
	if len(argv) > 0 && argv[0] == "--describe" {
		m := manifest{
			Name:        "plugin_echo",
			Description: "Echoes the message via plugin protocol",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"message": map[string]interface{}{"type": "string"},
				},
			},
		}
		_ = json.NewEncoder(os.Stdout).Encode(m)
		return
	}

	raw, _ := io.ReadAll(os.Stdin)
	var req struct {
		Args json.RawMessage ` + "`json:\"args\"`" + `
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		fmt.Println("bad request")
		os.Exit(1)
	}
	var args struct {
		Message string ` + "`json:\"message\"`" + `
	}
	_ = json.Unmarshal(req.Args, &args)
	_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"result": "echo: " + args.Message})
}
`
	if err := os.WriteFile(src, []byte(code), 0644); err != nil {
		t.Fatalf("write plugin source: %v", err)
	}

	exe := filepath.Join(dir, "plugin_helper")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", exe, src)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build plugin: %v: %s", err, out)
	}
	return exe
}

func TestLoadPluginManifest(t *testing.T) {
	exe := buildTestPlugin(t)

	plugin, err := LoadPlugin(exe, PluginOptions{Timeout: 30 * 1e9})
	if err != nil {
		t.Fatalf("LoadPlugin: %v", err)
	}
	if plugin.Name() != "plugin_echo" {
		t.Fatalf("name = %q", plugin.Name())
	}
	if plugin.Description() == "" {
		t.Fatal("empty description")
	}
	params := plugin.Parameters()
	if _, ok := params["properties"]; !ok {
		t.Fatalf("no properties in parameters: %v", params)
	}
}

func TestPluginExecute(t *testing.T) {
	exe := buildTestPlugin(t)

	plugin, err := LoadPlugin(exe, PluginOptions{Timeout: 30 * 1e9})
	if err != nil {
		t.Fatalf("LoadPlugin: %v", err)
	}

	res, err := plugin.Execute(fmt.Sprintf(`{"message": %q}`, "hello world"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res != "echo: hello world" {
		t.Fatalf("result = %q", res)
	}
}

func TestLoadPluginBrokenCommand(t *testing.T) {
	if _, err := LoadPlugin("definitely-not-a-real-command-xyz", PluginOptions{Timeout: 5 * 1e9}); err == nil {
		t.Fatal("expected error for broken plugin command")
	}
}

func TestRegisterPluginsConflictProtection(t *testing.T) {
	exe := buildTestPlugin(t)

	m := NewManager(t.TempDir(), []string{}, 0)
	var warnings []string
	m.RegisterPlugins([]string{exe}, func(format string, args ...interface{}) {
		warnings = append(warnings, fmt.Sprintf(format, args...))
	})

	// plugin_echo must be registered.
	if _, ok := m.tools["plugin_echo"]; !ok {
		t.Fatal("plugin not registered")
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}

	// A second registration attempt with a broken command must warn.
	m.RegisterPlugins([]string{"definitely-not-a-real-command-xyz"}, func(format string, args ...interface{}) {
		warnings = append(warnings, fmt.Sprintf(format, args...))
	})
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(warnings))
	}
}
