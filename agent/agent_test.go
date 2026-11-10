package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"agent/config"
	"agent/llm"
)

// mockResp is a canned LLM response: JSON for non-streaming requests,
// SSE body for streaming requests.
type mockResp struct {
	JSON string // body returned for "stream": false requests
	SSE  string // body returned for "stream": true requests
}

func funcCallResp(name, args string) string {
	return fmt.Sprintf(
		`{"choices":[{"message":{"role":"assistant","content":"","function_call":{"name":%q,"arguments":%s}}}]}`,
		name, args,
	)
}

func plainResp(content string) string {
	return fmt.Sprintf(
		`{"choices":[{"message":{"role":"assistant","content":%q}}]}`,
		content,
	)
}

func sseResp(chunks ...string) string {
	var b strings.Builder
	for _, c := range chunks {
		b.WriteString(fmt.Sprintf("data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", c))
	}
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

func sseFuncCallResp(name, args string) string {
	return fmt.Sprintf(
		"data: {\"choices\":[{\"delta\":{\"function_call\":{\"name\":%q,\"arguments\":%s}}}]}\n\ndata: [DONE]\n\n",
		name, args,
	)
}

// newTestAgent creates an agent pointed at a mock GigaChat-compatible server.
// The server returns the canned responses in order.
func newTestAgent(t *testing.T, responses []mockResp) (*Agent, *[]llm.Message) {
	t.Helper()

	var (
		mu        sync.Mutex
		lastMsgs  []llm.Message
		callCount int
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		if callCount >= len(responses) {
			http.Error(w, "no more canned responses", http.StatusInternalServerError)
			return
		}
		resp := responses[callCount]
		callCount++

		var req struct {
			Stream   bool          `json:"stream"`
			Messages []llm.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		lastMsgs = req.Messages

		if req.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(resp.SSE))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(resp.JSON))
	}))
	t.Cleanup(srv.Close)

	cfg := config.DefaultConfig()
	cfg.LLM.BaseURL = srv.URL
	cfg.Agent.SystemPrompt = "test-system-prompt"

	ag, err := New(cfg)
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	return ag, &lastMsgs
}

// counterTool is a simple tool used to verify the tool-calling loop.
type counterTool struct {
	calls int
}

func (c *counterTool) Name() string { return "counter" }
func (c *counterTool) Description() string {
	return "Counts how many times it was called."
}
func (c *counterTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}
func (c *counterTool) Execute(argsJSON string) (string, error) {
	c.calls++
	return fmt.Sprintf("call-%d", c.calls), nil
}

// TestProcessToolLoop verifies the agent can execute a chain of several tool
// calls before producing the final answer (regression for the early return
// that broke multi-step tool calling).
func TestProcessToolLoop(t *testing.T) {
	ag, lastMsgs := newTestAgent(t, []mockResp{
		{JSON: funcCallResp("counter", "{}")},
		{JSON: funcCallResp("counter", "{}")},
		{JSON: plainResp("done: call-2")},
	})

	tool := &counterTool{}
	ag.RegisterTool(tool)

	resp, err := ag.Process(context.Background(), "run the counter twice")
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if resp != "done: call-2" {
		t.Fatalf("expected 'done: call-2', got %q", resp)
	}
	if tool.calls != 2 {
		t.Fatalf("tool executed %d times, want 2", tool.calls)
	}

	// The final request must include the full function-call protocol:
	// system + user + assistant + function + assistant + function
	if len(*lastMsgs) != 6 {
		t.Fatalf("final request has %d messages, want 6", len(*lastMsgs))
	}
	if (*lastMsgs)[3].Role != "function" || (*lastMsgs)[3].Name != "counter" {
		t.Fatalf("message[3] role=%q name=%q, want function/counter",
			(*lastMsgs)[3].Role, (*lastMsgs)[3].Name)
	}

	// Regression: GigaChat requires the assistant message preceding each
	// function result to carry the function_call.
	for i, m := range *lastMsgs {
		if m.Role == "function" {
			if (*lastMsgs)[i-1].FunctionCall == nil {
				t.Fatalf("message[%d] precedes a function result but has no function_call", i-1)
			}
			if (*lastMsgs)[i-1].FunctionCall.Name != "counter" {
				t.Fatalf("function_call name = %q, want counter", (*lastMsgs)[i-1].FunctionCall.Name)
			}
		}
	}
}

// TestProcessNoTool verifies a plain response without tool calls.
func TestProcessNoTool(t *testing.T) {
	ag, _ := newTestAgent(t, []mockResp{
		{JSON: plainResp("hello!")},
	})

	resp, err := ag.Process(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if resp != "hello!" {
		t.Fatalf("expected 'hello!', got %q", resp)
	}
}

// TestProcessHistory verifies the session history records user and assistant
// messages (without breaking tool-protocol ordering) and respects the
// MaxHistoryMessages limit.
func TestProcessHistory(t *testing.T) {
	ag, _ := newTestAgent(t, []mockResp{
		{JSON: plainResp("one")},
		{JSON: plainResp("two")},
		{JSON: plainResp("three")},
	})
	ag.maxHistory = 4

	for _, msg := range []string{"a", "b", "c"} {
		if _, err := ag.Process(context.Background(), msg); err != nil {
			t.Fatalf("Process(%q) error: %v", msg, err)
		}
	}

	h := ag.History()
	if len(h) > ag.maxHistory {
		t.Fatalf("history has %d messages, want at most %d", len(h), ag.maxHistory)
	}
	if len(h) != 4 {
		t.Fatalf("history has %d messages, want 4", len(h))
	}
	// Oldest messages must be trimmed, newest preserved.
	if h[0].Role != "user" || h[0].Content != "b" {
		t.Fatalf("history[0] = %s/%q, want user/b", h[0].Role, h[0].Content)
	}
	if h[3].Role != "assistant" || h[3].Content != "three" {
		t.Fatalf("history[3] = %s/%q, want assistant/three", h[3].Role, h[3].Content)
	}
}

// TestProcessToolError verifies a tool failure is fed back to the model
// instead of aborting the loop.
func TestProcessToolError(t *testing.T) {
	ag, lastMsgs := newTestAgent(t, []mockResp{
		{JSON: funcCallResp("nonexistent_tool", "{}")},
		{JSON: plainResp("handled")},
	})

	resp, err := ag.Process(context.Background(), "do it")
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if resp != "handled" {
		t.Fatalf("expected 'handled', got %q", resp)
	}

	// The function result fed back must contain the error message.
	fnMsg := (*lastMsgs)[3]
	if fnMsg.Role != "function" || !strings.Contains(fnMsg.Content, "Error:") {
		t.Fatalf("function result %s/%q must contain error text", fnMsg.Role, fnMsg.Content)
	}
}

// TestProcessMaxTurns verifies the loop stops after maxTurns iterations.
func TestProcessMaxTurns(t *testing.T) {
	// Endless tool calls — the loop must stop.
	responses := make([]mockResp, 64)
	for i := range responses {
		responses[i] = mockResp{JSON: funcCallResp("counter", "{}")}
	}
	ag, _ := newTestAgent(t, responses)
	ag.maxTurns = 3

	_, err := ag.Process(context.Background(), "loop forever")
	if err == nil || !strings.Contains(err.Error(), "maximum tool-calling turns") {
		t.Fatalf("expected max-turns error, got %v", err)
	}
}

// TestProcessStream verifies streaming produces the full content via chunks.
func TestProcessStream(t *testing.T) {
	ag, _ := newTestAgent(t, []mockResp{
		{SSE: sseResp("hello", " ", "world")},
	})

	var got strings.Builder
	resp, err := ag.ProcessStream(context.Background(), "hi", func(chunk string) {
		got.WriteString(chunk)
	})
	if err != nil {
		t.Fatalf("ProcessStream returned error: %v", err)
	}
	if resp != "hello world" {
		t.Fatalf("expected 'hello world', got %q", resp)
	}
	if got.String() != "hello world" {
		t.Fatalf("chunks gave %q, want 'hello world'", got.String())
	}
}

// TestProcessStreamWithTool verifies streaming handles a tool call and
// continues to the final streamed answer.
func TestProcessStreamWithTool(t *testing.T) {
	ag, _ := newTestAgent(t, []mockResp{
		{SSE: sseFuncCallResp("echo", `{"message":"stream-echo"}`)},
		{SSE: sseResp("final", " answer")},
	})

	var got strings.Builder
	resp, err := ag.ProcessStream(context.Background(), "echo something", func(chunk string) {
		got.WriteString(chunk)
	})
	if err != nil {
		t.Fatalf("ProcessStream returned error: %v", err)
	}
	if resp != "final answer" {
		t.Fatalf("expected 'final answer', got %q", resp)
	}
	if got.String() != "final answer" {
		t.Fatalf("streamed chunks gave %q, want 'final answer'", got.String())
	}
}

// TestLoadHistory verifies LoadHistory sets and trims the session history.
func TestLoadHistory(t *testing.T) {
	ag, _ := newTestAgent(t, nil)
	ag.maxHistory = 2

	msgs := []llm.Message{
		{Role: "user", Content: "a"},
		{Role: "assistant", Content: "b"},
		{Role: "user", Content: "c"},
	}
	ag.LoadHistory(msgs)

	h := ag.History()
	if len(h) != 2 {
		t.Fatalf("history has %d messages, want 2", len(h))
	}
	if h[0].Content != "b" || h[1].Content != "c" {
		t.Fatalf("history not trimmed correctly: %v", h)
	}
}

// TestNormalizeArguments covers argument normalization variants.
func TestNormalizeArguments(t *testing.T) {
	cases := []struct {
		in   interface{}
		want string
	}{
		{nil, "{}"},
		{"", "{}"},
		{`{"a":1}`, `{"a":1}`},
		{map[string]interface{}{"a": 1.0}, `{"a":1}`},
	}
	for _, c := range cases {
		if got := normalizeArguments(c.in); got != c.want {
			t.Errorf("normalizeArguments(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestProcessUsageTracking verifies token usage from API responses is
// accumulated for the session.
func TestProcessUsageTracking(t *testing.T) {
	ag, _ := newTestAgent(t, []mockResp{
		{JSON: `{"choices":[{"message":{"role":"assistant","content":"one"}}],` +
			`"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`},
		{JSON: `{"choices":[{"message":{"role":"assistant","content":"two"}}],` +
			`"usage":{"prompt_tokens":20,"completion_tokens":5,"total_tokens":25}}`},
	})

	if _, err := ag.Process(context.Background(), "a"); err != nil {
		t.Fatalf("Process error: %v", err)
	}
	if _, err := ag.Process(context.Background(), "b"); err != nil {
		t.Fatalf("Process error: %v", err)
	}

	u := ag.Usage()
	if u.PromptTokens != 30 || u.CompletionTokens != 10 || u.TotalTokens != 40 {
		t.Fatalf("usage = %+v, want prompt=30 completion=10 total=40", u)
	}

	ag.ResetUsage()
	u = ag.Usage()
	if u.TotalTokens != 0 {
		t.Fatalf("usage not reset: %+v", u)
	}
}

// TestCompactHistorySummarize verifies LLM summarization replaces old
// messages when the token budget is exceeded.
func TestCompactHistorySummarize(t *testing.T) {
	ag, lastMsgs := newTestAgent(t, []mockResp{
		// First response: summary of the compacted history.
		{JSON: plainResp("User discussed the weather earlier.")},
		// Second response: the actual answer.
		{JSON: plainResp("answer")},
	})

	ag.summarize = true
	ag.contextTokens = 20 // tiny budget forces compaction

	// Long history well over the budget.
	ag.LoadHistory([]llm.Message{
		{Role: "user", Content: strings.Repeat("long message ", 50)},
		{Role: "assistant", Content: strings.Repeat("long answer ", 50)},
		{Role: "user", Content: strings.Repeat("long message ", 50)},
		{Role: "assistant", Content: strings.Repeat("long answer ", 50)},
	})

	resp, err := ag.Process(context.Background(), "continue")
	if err != nil {
		t.Fatalf("Process error: %v", err)
	}
	if resp != "answer" {
		t.Fatalf("unexpected answer: %q", resp)
	}

	h := ag.History()
	// [summary, 2 remaining long messages, user "continue", assistant "answer"]
	if len(h) != 5 {
		t.Fatalf("unexpected history length: %d messages", len(h))
	}
	if !strings.Contains(h[0].Content, "[Summary of earlier conversation]") {
		t.Fatalf("summary marker missing: %q", h[0].Content)
	}
	// Only the dropped half must be gone: exactly one remaining "long
	// message" and one "long answer" (the kept newer half).
	longMsgs := 0
	for _, m := range h[1:] {
		if strings.Contains(m.Content, strings.Repeat("long message ", 50)) {
			longMsgs++
		}
	}
	if longMsgs != 1 {
		t.Fatalf("expected 1 remaining long message, got %d", longMsgs)
	}

	// The main request (second call, stored in lastMsgs) must contain the
	// summary message.
	found := false
	for _, m := range *lastMsgs {
		if strings.Contains(m.Content, "[Summary of earlier conversation]") {
			found = true
		}
	}
	if !found {
		t.Fatal("summary message not present in main request")
	}
}

// TestCompactHistoryNoSummarize verifies the fallback: when summarization is
// disabled the history is simply trimmed without extra LLM calls.
func TestCompactHistoryNoSummarize(t *testing.T) {
	ag, _ := newTestAgent(t, []mockResp{
		{JSON: plainResp("ok")},
	})

	ag.summarize = false
	ag.contextTokens = 10

	ag.LoadHistory([]llm.Message{
		{Role: "user", Content: strings.Repeat("x ", 100)},
		{Role: "assistant", Content: strings.Repeat("y ", 100)},
	})

	if _, err := ag.Process(context.Background(), "hi"); err != nil {
		t.Fatalf("Process error: %v", err)
	}

	// No summary responses were queued — compaction must not have consumed
	// the only mock response reserved for the main request.
}
