package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// plugin protocol:
//
// A plugin is an external executable. It is invoked with a JSON request on
// stdin and must respond with a JSON reply on stdout (exit code 0).
//
// Manifest request:   argv[0] = "--describe"
//   reply:  {"name":"...", "description":"...", "parameters":{...}}
//
// Execute request:    stdin = {"args": <json args>, "raw": <original args json>}
//   reply:  {"result": "...", "error": ""}  (error non-empty = failure)

// PluginManifest is the plugin self-description returned by --describe.
type PluginManifest struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// pluginRequest is the JSON sent to a plugin's stdin on execution.
type pluginRequest struct {
	Args json.RawMessage `json:"args"`
	Raw  string          `json:"raw"`
}

// pluginResponse is the JSON a plugin must return.
type pluginResponse struct {
	Result string `json:"result"`
	Error  string `json:"error"`
}

// PluginTool is an external tool invoked through the subprocess protocol.
type PluginTool struct {
	command   string
	manifest  PluginManifest
	timeout   time.Duration
	overwrite bool // replaces a built-in tool with the same name
}

// PluginOptions tunes plugin behavior.
type PluginOptions struct {
	Timeout   time.Duration // default 30s
	Overwrite bool          // allow replacing built-in tools
}

// LoadPlugin probes the plugin command with --describe and returns a ready
// PluginTool. Returns an error when the command fails or returns a bad
// manifest.
func LoadPlugin(command string, opts PluginOptions) (*PluginTool, error) {
	if strings.TrimSpace(command) == "" {
		return nil, fmt.Errorf("plugin command is empty")
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}

	p := &PluginTool{command: command, timeout: opts.Timeout, overwrite: opts.Overwrite}

	out, err := p.run(context.Background(), []string{"--describe"}, nil)
	if err != nil {
		return nil, fmt.Errorf("plugin %q describe failed: %w", command, err)
	}

	var manifest PluginManifest
	if err := json.Unmarshal(out, &manifest); err != nil {
		return nil, fmt.Errorf("plugin %q returned invalid manifest: %w", command, err)
	}
	if strings.TrimSpace(manifest.Name) == "" {
		return nil, fmt.Errorf("plugin %q manifest has no name", command)
	}

	p.manifest = manifest
	return p, nil
}

// Name returns the tool name from the plugin manifest.
func (p *PluginTool) Name() string { return p.manifest.Name }

// Description returns the tool description from the plugin manifest.
func (p *PluginTool) Description() string {
	return p.manifest.Description + " (external plugin)"
}

// Parameters returns the parameters schema from the plugin manifest.
func (p *PluginTool) Parameters() map[string]interface{} {
	if p.manifest.Parameters == nil {
		return map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
	}
	return p.manifest.Parameters
}

// Overwrite reports whether this plugin may replace a built-in tool.
func (p *PluginTool) Overwrite() bool { return p.overwrite }

// Execute runs the plugin command with the JSON arguments.
func (p *PluginTool) Execute(argsJSON string) (string, error) {
	req, err := json.Marshal(pluginRequest{
		Args: json.RawMessage(argsJSON),
		Raw:  argsJSON,
	})
	if err != nil {
		return "", fmt.Errorf("marshal plugin request: %w", err)
	}

	out, err := p.run(context.Background(), nil, req)
	if err != nil {
		return "", err
	}

	var resp pluginResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		// Not a protocol reply — return raw output as the result.
		return truncateResult(strings.TrimSpace(string(out)), DefaultMaxResultSize), nil
	}
	if resp.Error != "" {
		return "", fmt.Errorf("plugin error: %s", resp.Error)
	}
	return truncateResult(resp.Result, DefaultMaxResultSize), nil
}

// run spawns the plugin process and returns its stdout.
func (p *PluginTool) run(ctx context.Context, argv []string, stdin []byte) ([]byte, error) {
	runCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, p.command, argv...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if runCtx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("plugin timed out after %v", p.timeout)
		}
		if msg != "" {
			return nil, fmt.Errorf("%w: %s", err, msg)
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

// RegisterPlugins loads all configured plugin commands into the manager.
// Broken plugins are skipped with a warning; overwrite=false protects
// built-ins.
func (m *Manager) RegisterPlugins(commands []string, warn func(format string, args ...interface{})) {
	registered := make(map[string]bool, len(m.tools))
	for name := range m.tools {
		registered[name] = true
	}

	for _, command := range commands {
		plugin, err := LoadPlugin(command, PluginOptions{Timeout: 30 * time.Second})
		if err != nil {
			if warn != nil {
				warn("plugin %q skipped: %v", command, err)
			}
			continue
		}
		if registered[plugin.Name()] && !plugin.Overwrite() {
			if warn != nil {
				warn("plugin %q skipped: name %q conflicts with a built-in tool", command, plugin.Name())
			}
			continue
		}
		m.Register(plugin)
	}
}
