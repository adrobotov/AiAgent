package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent/config"
)

// newTestServer creates a Server whose agents talk to a mock GigaChat server
// returning the given canned responses in order. Each response is a JSON body
// for non-streaming requests; the mock emits it as a single SSE chunk for
// streaming requests.
func newTestServer(t *testing.T, responses ...string) *Server {
	t.Helper()

	callIdx := 0
	mockLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if callIdx >= len(responses) {
			http.Error(w, "no more canned responses", http.StatusInternalServerError)
			return
		}
		body := responses[callIdx]
		callIdx++

		var req struct {
			Stream bool `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		if req.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			var parsed struct {
				Choices []struct {
					Message struct {
						Content string `json:"content"`
					} `json:"message"`
				} `json:"choices"`
			}
			content := ""
			if json.Unmarshal([]byte(body), &parsed) == nil && len(parsed.Choices) > 0 {
				content = parsed.Choices[0].Message.Content
			}
			contentJSON, _ := json.Marshal(content)
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":" +
				string(contentJSON) + "}}]}\n\ndata: [DONE]\n\n"))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(mockLLM.Close)

	cfg := config.DefaultConfig()
	cfg.LLM.BaseURL = mockLLM.URL
	return New(cfg)
}

func TestHealthz(t *testing.T) {
	srv := newTestServer(t)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("unexpected body: %v", body)
	}
}

func TestHandleChat(t *testing.T) {
	srv := newTestServer(t, `{"choices":[{"message":{"role":"assistant","content":"hello from agent"}}]}`)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/v1/chat", "application/json",
		strings.NewReader(`{"message":"hi","session_id":"s1"}`))
	if err != nil {
		t.Fatalf("POST /v1/chat: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body struct {
		SessionID string `json:"session_id"`
		Response  string `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Response != "hello from agent" {
		t.Fatalf("unexpected response: %q", body.Response)
	}
	if body.SessionID != "s1" {
		t.Fatalf("unexpected session: %q", body.SessionID)
	}
}

func TestHandleChatBadRequests(t *testing.T) {
	srv := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Invalid JSON
	resp, _ := http.Post(ts.URL+"/v1/chat", "application/json", strings.NewReader(`{`))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid JSON: status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Empty message
	resp, _ = http.Post(ts.URL+"/v1/chat", "application/json", strings.NewReader(`{"message":""}`))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty message: status = %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestHandleChatSessionIsolation(t *testing.T) {
	// Sessions reuse their agents: two requests to the same session share
	// the agent but consume separate LLM calls.
	srv := newTestServer(t,
		`{"choices":[{"message":{"role":"assistant","content":"first"}}]}`,
		`{"choices":[{"message":{"role":"assistant","content":"second"}}]}`,
	)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	post := func(session string) string {
		resp, err := http.Post(ts.URL+"/v1/chat", "application/json",
			strings.NewReader(`{"message":"hi","session_id":"`+session+`"}`))
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		defer resp.Body.Close()
		var body struct {
			Response string `json:"response"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return body.Response
	}

	if got := post("s1"); got != "first" {
		t.Fatalf("first call: got %q", got)
	}
	if got := post("s1"); got != "second" {
		t.Fatalf("same session second call: got %q", got)
	}
}

func TestHandleChatStream(t *testing.T) {
	srv := newTestServer(t, `{"choices":[{"message":{"role":"assistant","content":"streamed answer"}}]}`)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/v1/chat/stream", "application/json",
		strings.NewReader(`{"message":"hi"}`))
	if err != nil {
		t.Fatalf("POST /v1/chat/stream: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content type = %q", ct)
	}

	raw, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(raw), "delta") || !strings.Contains(string(raw), "done") {
		t.Fatalf("SSE body missing events: %q", raw)
	}
}

func TestHandleUsage(t *testing.T) {
	srv := newTestServer(t, `{"choices":[{"message":{"role":"assistant","content":"x"}}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	if _, err := http.Post(ts.URL+"/v1/chat", "application/json", strings.NewReader(`{"message":"hi"}`)); err != nil {
		t.Fatalf("POST: %v", err)
	}

	resp, err := http.Get(ts.URL + "/v1/usage")
	if err != nil {
		t.Fatalf("GET /v1/usage: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Usage.TotalTokens != 5 {
		t.Fatalf("usage = %d, want 5", body.Usage.TotalTokens)
	}
}
