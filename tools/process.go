package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ProcessTool provides process execution and management.
type ProcessTool struct {
	// Timeout is the maximum execution time for commands.
	// Default: 30 seconds.
	Timeout time.Duration

	// AllowedCommands restricts which commands can be executed.
	// If empty, no commands can be executed (deny by default).
	AllowedCommands []string

	// policy controls confirmation of dangerous operations.
	policy Policy
}

// DefaultAllowedCommands is the whitelist used when no explicit whitelist
// is configured.
var DefaultAllowedCommands = []string{
	"go", "git", "ls", "dir", "cat", "type", "echo", "pwd",
	"python", "node", "npm", "grep", "findstr",
}

// NewProcessTool creates a new process tool.
// If allowedCommands is nil, DefaultAllowedCommands is used (deny-by-default
// for anything outside the whitelist). Pass an explicit empty slice to allow
// no commands at all.
func NewProcessTool(timeout time.Duration, allowedCommands []string) *ProcessTool {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	if allowedCommands == nil {
		allowedCommands = DefaultAllowedCommands
	}
	return &ProcessTool{
		Timeout:         timeout,
		AllowedCommands: allowedCommands,
	}
}

// SetPolicy sets the confirmation policy for dangerous operations.
func (t *ProcessTool) SetPolicy(p Policy) {
	t.policy = p
}

// Name returns the tool name.
func (ProcessTool) Name() string {
	return "process"
}

// Description returns the tool description.
func (ProcessTool) Description() string {
	return "Executes external processes, lists running processes, and kills processes by PID."
}

// Parameters returns the tool parameters schema.
func (ProcessTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"exec", "ps", "kill"},
				"description": "The process operation to perform.",
			},
			"command": map[string]interface{}{
				"type":        "string",
				"description": "The command to execute (for 'exec' action).",
			},
			"args": map[string]interface{}{
				"type":        "string",
				"description": "Command line arguments (for 'exec' action).",
			},
			"pid": map[string]interface{}{
				"type":        "integer",
				"description": "Process ID (for 'kill' action).",
			},
		},
		"required": []string{"action"},
	}
}

// Execute executes the process operation.
func (t *ProcessTool) Execute(argsJSON string) (string, error) {
	var args struct {
		Action  string `json:"action"`
		Command string `json:"command"`
		Args    string `json:"args"`
		PID     int    `json:"pid"`
	}
	if err := parseArgs(argsJSON, &args); err != nil {
		return "", err
	}

	action := strings.ToLower(args.Action)

	// Dangerous operations require confirmation according to the policy.
	if action == "exec" || action == "kill" {
		detail := args.Command
		if detail == "" {
			detail = fmt.Sprintf("PID %d", args.PID)
		}
		if !t.policy.approve(t.Name(), action, detail) {
			return "", fmt.Errorf("operation '%s' (%s) was not approved (cancelled)", action, detail)
		}
	}

	switch action {
	case "exec":
		return t.execCommand(args.Command, args.Args)
	case "ps":
		return t.listProcesses()
	case "kill":
		return t.killProcess(args.PID)
	default:
		return "", fmt.Errorf("unknown action: %s. Available: exec, ps, kill", args.Action)
	}
}

// execCommand executes a command with optional arguments.
func (t *ProcessTool) execCommand(cmdStr, argsStr string) (string, error) {
	if cmdStr == "" {
		return "", fmt.Errorf("command is required")
	}

	// Check allowed commands: deny-by-default when the whitelist is empty.
	if len(t.AllowedCommands) == 0 {
		return "", fmt.Errorf("no commands are allowed (allowed_commands is empty)")
	}
	cmdName := extractCommand(cmdStr)
	allowed := false
	for _, allowedCmd := range t.AllowedCommands {
		if cmdName == allowedCmd {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", fmt.Errorf("command '%s' is not allowed. Allowed: %s", cmdName, strings.Join(t.AllowedCommands, ", "))
	}

	// Parse command and arguments
	var fullCmd string
	var fullArgs []string

	if argsStr != "" {
		fullCmd = cmdStr
		fullArgs = splitArgs(argsStr)
	} else {
		// Split the command string into command and args
		parts := splitArgs(cmdStr)
		if len(parts) == 0 {
			return "", fmt.Errorf("empty command")
		}
		fullCmd = parts[0]
		if len(parts) > 1 {
			fullArgs = parts[1:]
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), t.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, fullCmd, fullArgs...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	result := ""
	if stdout.Len() > 0 {
		result = stdout.String()
	}
	if stderr.Len() > 0 {
		if result != "" {
			result += "\n"
		}
		result += "stderr: " + stderr.String()
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("command timed out after %v", t.Timeout)
		}
		return "", fmt.Errorf("command failed (exit code %v): %s%s",
			cmd.ProcessState.ExitCode(), result,
			strings.TrimSuffix(result, "\n"))
	}

	return strings.TrimRight(result, "\n"), nil
}

// listProcesses lists running processes.
func (ProcessTool) listProcesses() (string, error) {
	// Try to get process list using system command
	var cmd *exec.Cmd
	switch os := getOSType(); os {
	case "windows":
		cmd = exec.Command("cmd", "/c", "tasklist", "/FI", "IMAGENAME eq *", "/FO", "CSV", "/NH")
	case "linux", "darwin":
		cmd = exec.Command("ps", "aux")
	default:
		return "", fmt.Errorf("unsupported operating system")
	}

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to list processes: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) > 50 {
		// Limit output
		lines = lines[:50]
		return strings.Join(lines, "\n") + "\n... (truncated, showing first 50)", nil
	}
	return strings.Join(lines, "\n"), nil
}

// killProcess terminates a process by PID.
func (ProcessTool) killProcess(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("invalid PID: %d", pid)
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return "", fmt.Errorf("failed to find process: %w", err)
	}

	err = proc.Kill()
	if err != nil {
		return "", fmt.Errorf("failed to kill process: %w", err)
	}

	return fmt.Sprintf("Process %d killed successfully", pid), nil
}

// extractCommand extracts the base command from a command string.
func extractCommand(cmdStr string) string {
	parts := strings.Fields(cmdStr)
	if len(parts) == 0 {
		return ""
	}
	// Handle "cmd /c args" pattern on Windows
	if parts[0] == "cmd" && len(parts) > 1 && parts[1] == "/c" && len(parts) > 2 {
		return parts[2]
	}
	return parts[0]
}

// splitArgs splits a command line into command and arguments.
// Handles simple quoting.
func splitArgs(s string) []string {
	var args []string
	var current strings.Builder
	var inQuotes bool

	for _, r := range s {
		switch r {
		case '"', '\'':
			inQuotes = !inQuotes
		case ' ', '\t':
			if inQuotes {
				current.WriteRune(r)
			} else if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}

// getOSType returns the operating system type.
func getOSType() string {
	switch sys := os.Getenv("GOOS"); sys {
	case "windows", "linux", "darwin":
		return sys
	default:
		// Fallback to runtime
		switch os.Getenv("OS") {
		case "Windows_NT":
			return "windows"
		default:
			return "linux"
		}
	}
}
