package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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
	ag.retryMinDelay = 0 // no waiting between retries
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
	if err == nil || !strings.Contains(err.Error(), "failed after 3 attempts") {
		t.Fatalf("expected give-up error, got %v", err)
	}
	if calls.Load() != 3 { // initial + 2 retries
		t.Fatalf("expected 3 calls, got %d", calls.Load())
	}
}

// TestBackoffCalculatesIncreasingDelay verifies exponential backoff grows.
func TestBackoffCalculatesIncreasingDelay(t *testing.T) {
	ag := &Agent{
		retryMinDelay:   100 * time.Millisecond,
		retryMaxDelay:   30 * time.Second,
		retryBackoffMul: 2.0,
	}

	d1 := ag.computeBackoff(1)
	d2 := ag.computeBackoff(2)
	d3 := ag.computeBackoff(3)

	// With jitter, exact values are unpredictable, but the order must hold.
	if d2 <= d1 {
		t.Errorf("backoff should increase: d1=%v d2=%v", d1, d2)
	}
	if d3 <= d2 {
		t.Errorf("backoff should increase: d2=%v d3=%v", d2, d3)
	}
}

// TestNonRetryableErrors verifies which errors are not retried.
func TestNonRetryableErrors(t *testing.T) {
	cases := []struct {
		err   string
		retry bool
	}{
		{"API error (status 401): unauthorized", false},
		{"API error (status 403): forbidden", false},
		{"API error (status 422): bad request", false},
		{"API error (status 400): invalid param", false},
		{"API error (status 500): internal error", true},
		{"API error (status 502): bad gateway", true},
		{"context deadline exceeded", true},
	}
	for _, tc := range cases {
		err := errors.New(tc.err)
		got := isNonRetryableError(err)
		if got != !tc.retry {
			t.Errorf("isNonRetryableError(%q) = %v, want %v", tc.err, got, !tc.retry)
		}
	}
}
