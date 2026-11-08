package history

import (
	"math"
	"testing"
)

func TestMemorySaveAndSearch(t *testing.T) {
	store, err := NewWithPath(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("store init: %v", err)
	}
	defer store.Close()

	// Simple 3-dimensional "embeddings".
	if err := store.SaveMemory("user likes Go", []float32{1, 0, 0}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := store.SaveMemory("user is from Moscow", []float32{0.9, 0.1, 0}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := store.SaveMemory("cat fact", []float32{0, 1, 0}); err != nil {
		t.Fatalf("save: %v", err)
	}
	// Memory without embedding must be ignored by search.
	if err := store.SaveMemory("no vector", nil); err != nil {
		t.Fatalf("save: %v", err)
	}

	results, err := store.SearchMemories([]float32{1, 0.05, 0}, 2)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Text != "user likes Go" {
		t.Fatalf("top result = %q, want %q", results[0].Text, "user likes Go")
	}
	if results[0].Score <= 0.99 {
		t.Fatalf("top score too low: %v", results[0].Score)
	}
	if results[1].Text != "user is from Moscow" {
		t.Fatalf("second result = %q", results[1].Text)
	}
}

func TestMemoryListAndDelete(t *testing.T) {
	store, err := NewWithPath(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("store init: %v", err)
	}
	defer store.Close()

	_ = store.SaveMemory("first", nil)
	_ = store.SaveMemory("second", nil)

	memories, err := store.ListMemories(10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(memories) != 2 {
		t.Fatalf("got %d memories, want 2", len(memories))
	}
	if memories[0].Text != "second" { // newest first
		t.Fatalf("newest first expected, got %q", memories[0].Text)
	}

	if err := store.DeleteMemories(); err != nil {
		t.Fatalf("delete: %v", err)
	}
	memories, _ = store.ListMemories(10)
	if len(memories) != 0 {
		t.Fatalf("memories not deleted: %d left", len(memories))
	}
}

func TestCosineSimilarity(t *testing.T) {
	same := cosineSimilarity([]float32{1, 0}, []float32{1, 0})
	if math.Abs(same-1) > 1e-9 {
		t.Errorf("identical vectors: %v, want 1", same)
	}

	orthogonal := cosineSimilarity([]float32{1, 0}, []float32{0, 1})
	if math.Abs(orthogonal) > 1e-9 {
		t.Errorf("orthogonal vectors: %v, want 0", orthogonal)
	}

	opposite := cosineSimilarity([]float32{1, 0}, []float32{-1, 0})
	if math.Abs(opposite+1) > 1e-9 {
		t.Errorf("opposite vectors: %v, want -1", opposite)
	}

	if cosineSimilarity([]float32{1, 2}, []float32{1}) != 0 {
		t.Error("mismatched lengths must return 0")
	}
	if cosineSimilarity(nil, nil) != 0 {
		t.Error("empty vectors must return 0")
	}
}

func TestEmbeddingEncoding(t *testing.T) {
	original := []float32{0.25, -1.5, 3.75, 0}
	blob := encodeEmbedding(original)
	if len(blob) != 4*len(original) {
		t.Fatalf("blob size = %d, want %d", len(blob), 4*len(original))
	}

	decoded := decodeEmbedding(blob)
	if len(decoded) != len(original) {
		t.Fatalf("decoded size = %d, want %d", len(decoded), len(original))
	}
	for i := range original {
		if decoded[i] != original[i] {
			t.Errorf("decoded[%d] = %v, want %v", i, decoded[i], original[i])
		}
	}

	if decodeEmbedding([]byte{1, 2, 3}) != nil {
		t.Error("invalid blob length must return nil")
	}
}
