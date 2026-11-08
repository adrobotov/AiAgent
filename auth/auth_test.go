package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newTestTokenManager creates a TokenManager backed by a mock OAuth server.
func newTestTokenManager(t *testing.T, calls *atomic.Int32, expiresIn time.Duration) (*TokenManager, *httptest.Server) {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)

		// Verify the request format.
		if r.Method != http.MethodPost {
			t.Errorf("auth request method = %s, want POST", r.Method)
		}
		if auth := r.Header.Get("Authorization"); !strings.HasPrefix(auth, "Basic ") {
			t.Errorf("auth header = %q, want Basic prefix", auth)
		}
		if r.Header.Get("RqUID") == "" {
			t.Error("RqUID header is empty")
		}
		if err := r.ParseForm(); err != nil || r.FormValue("scope") == "" {
			t.Errorf("scope form value missing: err=%v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(TokenResponse{
			AccessToken: "token-for-test",
			ExpiresAt:   time.Now().Add(30 * time.Minute).UnixMilli(),
		})
	}))
	t.Cleanup(srv.Close)

	cfg := &OAuthConfig{
		AuthURL: srv.URL,
		Scope:   ScopePers,
		Timeout: 5 * time.Second,
	}
	tm := NewTokenManager(cfg, "test-auth-key")
	return tm, srv
}

func TestTokenManagerGetToken(t *testing.T) {
	var calls atomic.Int32
	tm, _ := newTestTokenManager(t, &calls, 30*time.Minute)

	ctx := context.Background()

	token1, err := tm.GetToken(ctx)
	if err != nil {
		t.Fatalf("GetToken error: %v", err)
	}
	if token1 == "" {
		t.Fatal("empty token")
	}

	// Second call must be cached — no new server request.
	token2, err := tm.GetToken(ctx)
	if err != nil {
		t.Fatalf("GetToken error: %v", err)
	}
	if token1 != token2 {
		t.Fatalf("token changed between calls: %q vs %q", token1, token2)
	}
	if calls.Load() != 1 {
		t.Fatalf("server called %d times, want 1 (token must be cached)", calls.Load())
	}
}

func TestTokenManagerRefreshOnExpiry(t *testing.T) {
	var calls atomic.Int32
	tm, _ := newTestTokenManager(t, &calls, 30*time.Minute)

	ctx := context.Background()
	if _, err := tm.GetToken(ctx); err != nil {
		t.Fatalf("GetToken error: %v", err)
	}

	// Force expiry.
	tm.mu.Lock()
	tm.expired = time.Now().Add(-time.Minute)
	tm.mu.Unlock()

	if _, err := tm.GetToken(ctx); err != nil {
		t.Fatalf("GetToken after expiry error: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("server called %d times, want 2 (refresh after expiry)", calls.Load())
	}
}

func TestAuthHeaderBase64(t *testing.T) {
	// A valid base64 key is used as-is with the Basic prefix.
	var gotAuth atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth.Store(r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(TokenResponse{AccessToken: "t", ExpiresAt: time.Now().Add(time.Hour).UnixMilli()})
	}))
	defer srv.Close()

	cfg := &OAuthConfig{AuthURL: srv.URL, Scope: ScopePers, Timeout: 5 * time.Second}
	tm := NewTokenManager(cfg, "dXNlcjpwYXNz") // base64("user:pass")
	if _, err := tm.GetToken(context.Background()); err != nil {
		t.Fatalf("GetToken error: %v", err)
	}

	auth := gotAuth.Load().(string)
	if auth != "Basic dXNlcjpwYXNz" {
		t.Fatalf("auth header = %q, want Basic dXNlcjpwYXNz", auth)
	}
}
