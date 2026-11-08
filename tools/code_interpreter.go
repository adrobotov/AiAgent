package tools

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// CodeInterpreterTool executes Go code snippets safely.
type CodeInterpreterTool struct {
	timeout time.Duration
}

// NewCodeInterpreterTool creates a new code interpreter tool.
func NewCodeInterpreterTool() *CodeInterpreterTool {
	return &CodeInterpreterTool{
		timeout: 10 * time.Second,
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

// executeCode compiles and runs Go code.
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

	// Create temp file
	tmpFile, err := os.CreateTemp("", "code_*.go")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(wrappedCode); err != nil {
		return "", fmt.Errorf("write code: %w", err)
	}
	tmpFile.Close()

	// Compile
	compileCmd := exec.Command("go", "build", "-o", tmpFile.Name()+".exe", tmpFile.Name())
	var compileBuf bytes.Buffer
	compileCmd.Stdout = &compileBuf
	compileCmd.Stderr = &compileBuf

	if err := compileCmd.Run(); err != nil {
		return "", fmt.Errorf("compile error: %s", compileBuf.String())
	}

	// Execute with timeout
	execCmd := exec.Command(tmpFile.Name() + ".exe")
	var stdout, stderr bytes.Buffer
	execCmd.Stdout = &stdout
	execCmd.Stderr = &stderr

	done := make(chan error, 1)
	go func() {
		done <- execCmd.Run()
	}()

	select {
	case err := <-done:
		if err != nil {
			if stderr.Len() > 0 {
				return "", fmt.Errorf("runtime error: %s", stderr.String())
			}
			return "", fmt.Errorf("execution error: %v", err)
		}
	case <-time.After(t.timeout):
		execCmd.Process.Kill()
		return "", fmt.Errorf("timeout after %v", t.timeout)
	}

	output := strings.TrimSpace(stdout.String())
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
