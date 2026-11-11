package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	codeCompileTimeout = 30 * time.Second
	codeRunTimeout     = 10 * time.Second
	codeMaxOutput      = 10000 // max chars of stdout/stderr returned
)

// CodeInterpreterTool executes Go code snippets in an isolated temp directory.
type CodeInterpreterTool struct {
	compileTimeout time.Duration
	runTimeout     time.Duration
}

// NewCodeInterpreterTool creates a new code interpreter tool.
func NewCodeInterpreterTool() *CodeInterpreterTool {
	return &CodeInterpreterTool{
		compileTimeout: codeCompileTimeout,
		runTimeout:     codeRunTimeout,
	}
}

// Name returns the tool name.
func (t *CodeInterpreterTool) Name() string {
	return "code_interpreter"
}

// Description returns the tool description.
func (t *CodeInterpreterTool) Description() string {
	return "Executes Go code and returns the output. Use for complex calculations or data processing."
}

// Parameters returns the tool parameters.
func (t *CodeInterpreterTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"code": map[string]interface{}{
				"type":        "string",
				"description": "The Go code to execute.",
			},
		},
		"required": []string{"code"},
	}
}

// Execute executes the Go code.
func (t *CodeInterpreterTool) Execute(argsJSON string) (string, error) {
	var args struct {
		Code string `json:"code"`
	}
	if err := parseArgs(argsJSON, &args); err != nil {
		return "", err
	}

	return t.executeCode(args.Code)
}

// executeCode compiles and runs Go code in an isolated temporary directory.
// The compile step and the run step each have their own timeout; the output
// is limited to codeMaxOutput characters.
func (t *CodeInterpreterTool) executeCode(code string) (string, error) {
	// Wrap code in a complete Go program
	wrappedCode := fmt.Sprintf(`package main

import (
	"fmt"
	"time"
)

func main() {
	%s
}
`, code)

	// Isolated temp directory, removed after execution
	tmpDir, err := os.MkdirTemp("", "agent-code-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	srcPath := filepath.Join(tmpDir, "main.go")
	binPath := filepath.Join(tmpDir, "main.bin")

	if err := os.WriteFile(srcPath, []byte(wrappedCode), 0644); err != nil {
		return "", fmt.Errorf("write code: %w", err)
	}

	// Compile with timeout
	compileCtx, compileCancel := context.WithTimeout(context.Background(), t.compileTimeout)
	defer compileCancel()

	compileCmd := exec.CommandContext(compileCtx, "go", "build", "-o", binPath, srcPath)
	var compileBuf bytes.Buffer
	compileCmd.Stdout = &compileBuf
	compileCmd.Stderr = &compileBuf

	if err := compileCmd.Run(); err != nil {
		out := truncateResult(strings.TrimSpace(compileBuf.String()), codeMaxOutput)
		if compileCtx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("compile timed out after %v", t.compileTimeout)
		}
		return "", fmt.Errorf("compile error: %s", out)
	}

	// Execute with timeout
	runCtx, runCancel := context.WithTimeout(context.Background(), t.runTimeout)
	defer runCancel()

	execCmd := exec.CommandContext(runCtx, binPath)
	var stdout, stderr bytes.Buffer
	execCmd.Stdout = &stdout
	execCmd.Stderr = &stderr

	if err := execCmd.Run(); err != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("timeout after %v", t.runTimeout)
		}
		if stderr.Len() > 0 {
			return "", fmt.Errorf("runtime error: %s", truncateResult(strings.TrimSpace(stderr.String()), codeMaxOutput))
		}
		return "", fmt.Errorf("execution error: %v", err)
	}

	output := truncateResult(strings.TrimSpace(stdout.String()), codeMaxOutput)
	if output == "" {
		output = "(no output)"
	}

	return output, nil
}

// ExecuteCode is a convenience function for code execution.
func ExecuteCode(code string) (string, error) {
	tool := NewCodeInterpreterTool()
	return tool.executeCode(code)
}
