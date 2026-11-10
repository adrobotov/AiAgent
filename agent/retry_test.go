package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"agent/config"
	"agent/llm"
)

// retryAgent builds an agent pointed at the given handler.
func retryAgent(t *testing.T, handler http.HandlerFunc) *Agent {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfg := config.DefaultConfig()
	cfg.LLM.BaseURL = srv.URL
	ag, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ag.retryDelay = 0 // no waiting between retries
	return ag
}

func TestChatWithRetryNoRetryOn401(t *testing.T) {
	var calls atomic.Int32
	ag := retryAgent(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "unauthorized"})
	})

	_, err := ag.chatWithRetry(context.Background(), []llm.Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected 401 error")
	}
	if calls.Load() != 1 {
		t.Fatalf("401 must not be retried, got %d calls", calls.Load())
	}
}

func TestChatWithRetryRetriesOn500(t *testing.T) {
	var calls atomic.Int32
	ag := retryAgent(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("temporary failure"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"recovered"}}]}`))
	})

	resp, err := ag.chatWithRetry(context.Background(), []llm.Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("chatWithRetry: %v", err)
	}
	if resp.Content != "recovered" {
		t.Fatalf("content = %q", resp.Content)
	}
	if calls.Load() != 3 {
		t.Fatalf("expected 3 calls, got %d", calls.Load())
	}
}

func TestChatWithRetryGivesUp(t *testing.T) {
	var calls atomic.Int32
	ag := retryAgent(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("always failing"))
	})
	ag.retryCount = 2

	_, err := ag.chatWithRetry(context.Background(), []llm.Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "failed after 2 attempts") {
		t.Fatalf("expected give-up error, got %v", err)
	}
	if calls.Load() != 3 { // initial + 2 retries
		t.Fatalf("expected 3 calls, got %d", calls.Load())
	}
}
