package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"agent/config"
)

// TestChatParsesResponse verifies Chat parses content, function_call and usage.
func TestChatParsesResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices": [{"message": {"role": "assistant", "content": "hi there"}}],
			"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
		}`))
	}))
	defer srv.Close()

	client := NewClient(testConfig(srv.URL))
	resp, err := client.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if resp.Content != "hi there" {
		t.Fatalf("unexpected content: %q", resp.Content)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 15 {
		t.Fatalf("usage not parsed: %+v", resp.Usage)
	}
}

func TestChatParsesFunctionCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices": [{"message": {"role": "assistant", "content": "",
				"function_call": {"name": "calc", "arguments": {"expression": "2+2"}}}}]
		}`))
	}))
	defer srv.Close()

	client := NewClient(testConfig(srv.URL))
	resp, err := client.Chat(context.Background(), []Message{{Role: "user", Content: "calc"}}, nil)
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if resp.FunctionCall == nil {
		t.Fatal("function call not parsed")
	}
	if resp.FunctionCall.Name != "calc" {
		t.Fatalf("unexpected function name: %s", resp.FunctionCall.Name)
	}
}

func TestChatErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewClient(testConfig(srv.URL))
	_, err := client.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected status error, got %v", err)
	}
}

func TestStreamContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(
			"data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n" +
				"data: {\"choices\":[{\"delta\":{\"content\":\" world\"}}]}\n\n" +
				"data: [DONE]\n\n"))
	}))
	defer srv.Close()

	client := NewClient(testConfig(srv.URL))
	ch, err := client.Stream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("Stream error: %v", err)
	}

	var got strings.Builder
	var complete bool
	for chunk := range ch {
		got.WriteString(chunk.Content)
		if chunk.IsComplete {
			complete = true
		}
	}
	if got.String() != "hello world" {
		t.Fatalf("streamed %q, want %q", got.String(), "hello world")
	}
	if !complete {
		t.Fatal("stream never completed")
	}
}

func TestStreamFunctionCallFragments(t *testing.T) {
	// Arguments streamed as fragments across deltas.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(
			"data: {\"choices\":[{\"delta\":{\"function_call\":{\"name\":\"calc\",\"arguments\":\"{\\\"expr\"}}}]}\n\n" +
				"data: {\"choices\":[{\"delta\":{\"function_call\":{\"arguments\":\"ession\\\":\\\"2+2\\\"}\"}}}]}\n\n" +
				"data: [DONE]\n\n"))
	}))
	defer srv.Close()

	client := NewClient(testConfig(srv.URL))
	ch, err := client.Stream(context.Background(), []Message{{Role: "user", Content: "calc"}}, nil)
	if err != nil {
		t.Fatalf("Stream error: %v", err)
	}

	var call *FunctionCall
	for chunk := range ch {
		if chunk.FunctionCall != nil {
			call = chunk.FunctionCall
		}
	}
	if call == nil {
		t.Fatal("function call not received")
	}
	if call.Name != "calc" {
		t.Fatalf("unexpected name: %s", call.Name)
	}
	args, ok := call.Arguments.(string)
	if !ok || args != `{"expression":"2+2"}` {
		t.Fatalf("unexpected arguments: %v", call.Arguments)
	}
}

func TestStreamUsageChunk(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(
			"data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n" +
				"data: {\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\n" +
				"data: [DONE]\n\n"))
	}))
	defer srv.Close()

	client := NewClient(testConfig(srv.URL))
	ch, err := client.Stream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("Stream error: %v", err)
	}

	var usage *Usage
	for chunk := range ch {
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
	}
	if usage == nil || usage.TotalTokens != 5 {
		t.Fatalf("usage chunk not parsed: %+v", usage)
	}
}

func TestMergeFunctionCalls(t *testing.T) {
	// nil accumulator
	got := mergeFunctionCalls(nil, &FunctionCall{Name: "a", Arguments: `{"x":1}`})
	if got.Name != "a" {
		t.Fatalf("unexpected name: %s", got.Name)
	}

	// accumulated JSON is replaced by a newer valid JSON
	got = mergeFunctionCalls(&FunctionCall{Name: "a", Arguments: `{"x":`}, &FunctionCall{Arguments: `{"x":1}`})
	if s, _ := got.Arguments.(string); s != `{"x":1}` {
		t.Fatalf("accumulated JSON not replaced: %v", got.Arguments)
	}

	// fragments are concatenated
	got = mergeFunctionCalls(&FunctionCall{Name: "a", Arguments: `{"x":`}, &FunctionCall{Arguments: `1}`})
	if s, _ := got.Arguments.(string); s != `{"x":1}` {
		t.Fatalf("fragments not concatenated: %v", got.Arguments)
	}
}

func TestModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/models") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"GigaChat-3-Ultra","owned_by":"sber"},{"id":"GigaChat-2"}]}`))
	}))
	defer srv.Close()

	client := NewClient(testConfig(srv.URL))
	models, err := client.Models(context.Background())
	if err != nil {
		t.Fatalf("Models error: %v", err)
	}
	if len(models) != 2 || models[0].ID != "GigaChat-3-Ultra" || models[1].ID != "GigaChat-2" {
		t.Fatalf("unexpected models: %+v", models)
	}
}

func TestSetModel(t *testing.T) {
	var gotModel atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotModel.Store(req.Model)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer srv.Close()

	client := NewClient(testConfig(srv.URL))
	if client.Model() != "test-model" {
		t.Fatalf("initial model = %q", client.Model())
	}

	client.SetModel("GigaChat-3-Ultra")
	if client.Model() != "GigaChat-3-Ultra" {
		t.Fatalf("model after SetModel = %q", client.Model())
	}

	if _, err := client.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got := gotModel.Load().(string); got != "GigaChat-3-Ultra" {
		t.Fatalf("request model = %q, want GigaChat-3-Ultra", got)
	}
}

func TestEmbed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
			Input string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Model != "embeddings" {
			http.Error(w, "wrong model", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.5,-0.25,1.0]}]}`))
	}))
	defer srv.Close()

	client := NewClient(testConfig(srv.URL))
	vec, err := client.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed error: %v", err)
	}
	if len(vec) != 3 || vec[0] != 0.5 || vec[1] != -0.25 || vec[2] != 1.0 {
		t.Fatalf("unexpected embedding: %v", vec)
	}
}

// testConfig builds an LLM config pointing at the given base URL.
func testConfig(baseURL string) *config.LLMConfig {
	return &config.LLMConfig{
		BaseURL:     baseURL,
		Model:       "test-model",
		Temperature: 0.5,
		MaxTokens:   100,
	}
}
