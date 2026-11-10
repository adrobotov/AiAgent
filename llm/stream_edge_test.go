package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStreamNoDoneMarker(t *testing.T) {
	// Server closes the stream without [DONE] — the client must still emit
	// an IsComplete chunk.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"tail\"}}]}\n\n"))
	}))
	defer srv.Close()

	client := NewClient(testConfig(srv.URL))
	ch, err := client.Stream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var content strings.Builder
	complete := false
	for chunk := range ch {
		content.WriteString(chunk.Content)
		if chunk.IsComplete {
			complete = true
		}
	}
	if content.String() != "tail" || !complete {
		t.Fatalf("content=%q complete=%v", content.String(), complete)
	}
}

func TestStreamSkipsCommentsAndBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(
			": keep-alive comment\n\n" +
				"data: {broken json}\n\n" +
				"data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n" +
				"data: [DONE]\n\n"))
	}))
	defer srv.Close()

	client := NewClient(testConfig(srv.URL))
	ch, err := client.Stream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var content strings.Builder
	for chunk := range ch {
		content.WriteString(chunk.Content)
	}
	if content.String() != "ok" {
		t.Fatalf("content=%q, want ok", content.String())
	}
}

func TestStreamErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusUnauthorized)
	}))
	defer srv.Close()

	client := NewClient(testConfig(srv.URL))
	if _, err := client.Stream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil); err == nil ||
		!strings.Contains(err.Error(), "401") {
		t.Fatalf("expected 401 error, got %v", err)
	}
}

func TestModelsErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "denied", http.StatusForbidden)
	}))
	defer srv.Close()

	client := NewClient(testConfig(srv.URL))
	if _, err := client.Models(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "403") {
		t.Fatalf("expected 403 error, got %v", err)
	}
}

func TestKnownModels(t *testing.T) {
	models := KnownModels()
	if len(models) == 0 {
		t.Fatal("KnownModels must not be empty")
	}
	for _, m := range models {
		if !strings.HasPrefix(m, "GigaChat") {
			t.Fatalf("unexpected model name: %q", m)
		}
	}
}
