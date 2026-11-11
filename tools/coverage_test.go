package tools

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// --- parseArgs ---

func TestParseArgsNonJSON(t *testing.T) {
	var target struct {
		Value string `json:"value"`
	}
	if err := parseArgs(`plain string`, &target); err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if target.Value != "plain string" {
		t.Fatalf("value = %q", target.Value)
	}
}

func TestParseArgsJSON(t *testing.T) {
	var target struct {
		Message string `json:"message"`
	}
	if err := parseArgs(`{"message":"hi"}`, &target); err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if target.Message != "hi" {
		t.Fatalf("message = %q", target.Message)
	}
}

// --- built-in tool helpers ---

func TestExecuteEcho(t *testing.T) {
	res, err := ExecuteEcho(`{"message":"echo me"}`)
	if err != nil {
		t.Fatalf("ExecuteEcho: %v", err)
	}
	if res != "echo me" {
		t.Fatalf("result = %q", res)
	}
}

func TestGetCurrentTime(t *testing.T) {
	res, err := GetCurrentTime()
	if err != nil {
		t.Fatalf("GetCurrentTime: %v", err)
	}
	if res == "" {
		t.Fatal("empty time")
	}
}

func TestCalculateViaExecute(t *testing.T) {
	m := NewManager(t.TempDir(), []string{}, 0, "", false)
	res, err := m.Execute("calc", `{"expression":"2+2*10"}`)
	if err != nil {
		t.Fatalf("calc: %v", err)
	}
	if res != "22.0000" {
		t.Fatalf("calc result = %q", res)
	}
}

// --- filesystem actions via Execute ---

func TestFileSystemActions(t *testing.T) {
	root := t.TempDir()
	tool := NewFileSystemTool(root, false)
	tool.SetPolicy(Policy{Mode: ConfirmAlways})

	exec := func(args string) string {
		t.Helper()
		res, err := tool.Execute(args)
		if err != nil {
			t.Fatalf("Execute(%s): %v", args, err)
		}
		return res
	}

	// mkdir + write
	exec(`{"action":"mkdir","path":"sub"}`)
	exec(`{"action":"write","path":"sub/data.txt","content":"data"}`)

	// exists
	if res := exec(`{"action":"exists","path":"sub/data.txt"}`); !strings.Contains(res, "true") {
		t.Fatalf("exists: %q", res)
	}

	// read
	if res := exec(`{"action":"read","path":"sub/data.txt"}`); !strings.Contains(res, "data") {
		t.Fatalf("read: %q", res)
	}

	// info
	if res := exec(`{"action":"info","path":"sub/data.txt"}`); !strings.Contains(res, "4") {
		t.Fatalf("info: %q", res)
	}

	// copy
	exec(`{"action":"copy","path":"sub/data.txt","destination":"sub/copy.txt"}`)
	if res := exec(`{"action":"exists","path":"sub/copy.txt"}`); !strings.Contains(res, "true") {
		t.Fatalf("copy failed: %q", res)
	}

	// move
	exec(`{"action":"move","path":"sub/copy.txt","destination":"sub/moved.txt"}`)
	if res := exec(`{"action":"exists","path":"sub/moved.txt"}`); !strings.Contains(res, "true") {
		t.Fatalf("move failed: %q", res)
	}

	// list
	if res := exec(`{"action":"list","path":"sub"}`); !strings.Contains(res, "moved.txt") {
		t.Fatalf("list: %q", res)
	}

	// delete
	exec(`{"action":"delete","path":"sub/moved.txt"}`)
	if res := exec(`{"action":"exists","path":"sub/moved.txt"}`); strings.Contains(res, "true") {
		t.Fatalf("delete failed")
	}
}

func TestFileSystemEmptyDir(t *testing.T) {
	tool := NewFileSystemTool(t.TempDir(), false)
	tool.SetPolicy(Policy{Mode: ConfirmAlways})
	res, err := tool.Execute(`{"action":"list","path":""}`)
	if err != nil {
		t.Fatalf("list empty root: %v", err)
	}
	// Root always contains something or reports empty — both acceptable.
	_ = res
}

func TestFileSystemUnknownAction(t *testing.T) {
	tool := NewFileSystemTool(t.TempDir(), false)
	tool.SetPolicy(Policy{Mode: ConfirmAlways})
	if _, err := tool.Execute(`{"action":"chmod","path":"x"}`); err == nil ||
		!strings.Contains(err.Error(), "unknown action") {
		t.Fatalf("expected unknown action error, got %v", err)
	}
}

// --- process tool ---

func TestProcessListProcesses(t *testing.T) {
	tool := NewProcessTool(10*time.Second, []string{"go"}, false)
	tool.SetPolicy(Policy{Mode: ConfirmAlways})

	res, err := tool.Execute(`{"action":"ps"}`)
	if err != nil {
		t.Skipf("ps not supported on this environment: %v", err)
	}
	if res == "" {
		t.Fatal("empty process list")
	}
}

func TestProcessKillInvalidPID(t *testing.T) {
	tool := NewProcessTool(5*time.Second, []string{"go"}, false)
	tool.SetPolicy(Policy{Mode: ConfirmAlways})

	// PID 0 and negative are invalid; a huge PID does not exist.
	if _, err := tool.Execute(`{"action":"kill","pid":-1}`); err == nil {
		t.Fatal("expected error for negative PID")
	}
}

func TestProcessUnknownAction(t *testing.T) {
	tool := NewProcessTool(5*time.Second, []string{"go"}, false)
	tool.SetPolicy(Policy{Mode: ConfirmAlways})

	if _, err := tool.Execute(`{"action":"pause"}`); err == nil ||
		!strings.Contains(err.Error(), "unknown action") {
		t.Fatalf("expected unknown action error, got %v", err)
	}
}

// --- web search parser ---

func TestParseDuckDuckGoResults(t *testing.T) {
	html := `<div class="result">
<div class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fpage">Example Title</a>
<a class="result__snippet" href="#">Example snippet text</a>
</div>
<div class="result">
<div class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.org">No Snippet Title</a>
</div>`

	results, err := parseDuckDuckGoResults(html, 10)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("no results parsed")
	}
	if results[0].Title != "Example Title" {
		t.Fatalf("title = %q", results[0].Title)
	}
	if results[0].Snippet != "Example snippet text" {
		t.Fatalf("snippet = %q", results[0].Snippet)
	}
	if !strings.Contains(results[0].URL, "example.com") {
		t.Fatalf("url = %q", results[0].URL)
	}
}

func TestParseDuckDuckGoMaxResults(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 10; i++ {
		fmt.Fprintf(&b, `<div class="result__a" href="//x">Title %d</a>`, i)
	}
	results, err := parseDuckDuckGoResults(b.String(), 3)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(results) > 3 {
		t.Fatalf("got %d results, want <= 3", len(results))
	}
}

func TestWebSearchToolEmptyQuery(t *testing.T) {
	// An empty query reaches the endpoint and may fail or return empty
	// results — must not panic.
	tool := NewWebSearchTool()
	tool.client.Timeout = 5 * time.Second
	if _, err := tool.Execute(`{"query":""}`); err != nil {
		t.Logf("empty query error (acceptable): %v", err)
	}
}

// --- code interpreter (integration, requires the Go toolchain) ---

func TestExecuteCodeIntegration(t *testing.T) {
	res, err := ExecuteCode("fmt.Println(\"interpreter ok\")")
	if err != nil {
		t.Skipf("code interpreter unavailable: %v", err)
	}
	if !strings.Contains(res, "interpreter ok") {
		t.Fatalf("output = %q", res)
	}
}

func TestExecuteCodeCompileError(t *testing.T) {
	_, err := ExecuteCode("this is not valid go code")
	if err == nil || !strings.Contains(err.Error(), "compile error") {
		t.Fatalf("expected compile error, got %v", err)
	}
}

// --- weather helpers ---

func TestMockWeather(t *testing.T) {
	res := mockWeather("Testville", fmt.Errorf("down"))
	if !strings.Contains(res, "Testville") || !strings.Contains(res, "данные недоступны") {
		t.Fatalf("unexpected mock: %q", res)
	}
}

func TestFetchWeatherEmptyLocation(t *testing.T) {
	tool := NewWeatherTool("")
	if _, err := tool.Execute(`{"location":""}`); err == nil {
		t.Fatal("expected error for empty location")
	}
}
