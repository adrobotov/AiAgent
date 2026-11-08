package history

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"time"
)

// Memory represents a long-term memory entry with its embedding.
type Memory struct {
	ID        int64
	Text      string
	CreatedAt time.Time
	Score     float64 // similarity score (filled by SearchMemories)
}

// initMemoryDB creates the memories table.
func (s *Store) initMemoryDB() error {
	query := `
	CREATE TABLE IF NOT EXISTS memories (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		text TEXT NOT NULL,
		embedding BLOB,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`
	_, err := s.db.Exec(query)
	if err != nil {
		return fmt.Errorf("create memories table: %w", err)
	}
	return nil
}

// SaveMemory stores a memory entry with its embedding vector.
// If embedding is nil, the memory is stored without a vector (it will not
// participate in similarity search).
func (s *Store) SaveMemory(text string, embedding []float32) error {
	var blob []byte
	if len(embedding) > 0 {
		blob = encodeEmbedding(embedding)
	}
	_, err := s.db.Exec(
		"INSERT INTO memories (text, embedding) VALUES (?, ?)",
		text, blob,
	)
	return err
}

// SearchMemories returns the most similar memories to the query embedding,
// ordered by cosine similarity (descending). Only memories with embeddings
// are considered.
func (s *Store) SearchMemories(query []float32, limit int) ([]Memory, error) {
	if len(query) == 0 || limit <= 0 {
		return nil, nil
	}

	rows, err := s.db.Query(
		"SELECT id, text, embedding, created_at FROM memories WHERE embedding IS NOT NULL",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var scored []Memory
	for rows.Next() {
		var m Memory
		var blob []byte
		var createdAt string
		if err := rows.Scan(&m.ID, &m.Text, &blob, &createdAt); err != nil {
			return nil, err
		}
		if m.CreatedAt, err = parseDBTime(createdAt); err != nil {
			return nil, err
		}

		vec := decodeEmbedding(blob)
		m.Score = cosineSimilarity(query, vec)
		scored = append(scored, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	if len(scored) > limit {
		scored = scored[:limit]
	}
	return scored, nil
}

// ListMemories returns recent memories ordered by creation date (newest first).
func (s *Store) ListMemories(limit int) ([]Memory, error) {
	rows, err := s.db.Query(
		"SELECT id, text, NULL, created_at FROM memories ORDER BY id DESC LIMIT ?",
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []Memory
	for rows.Next() {
		var m Memory
		var createdAt string
		var blob []byte
		if err := rows.Scan(&m.ID, &m.Text, &blob, &createdAt); err != nil {
			return nil, err
		}
		if m.CreatedAt, err = parseDBTime(createdAt); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

// DeleteMemories removes all long-term memories.
func (s *Store) DeleteMemories() error {
	_, err := s.db.Exec("DELETE FROM memories")
	if err != nil {
		return err
	}
	s.db.Exec("DELETE FROM sqlite_sequence WHERE name='memories'")
	return nil
}

// cosineSimilarity computes the cosine similarity between two vectors.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / math.Sqrt(normA*normB)
}

// encodeEmbedding serializes a float32 vector to a little-endian byte blob.
func encodeEmbedding(vec []float32) []byte {
	blob := make([]byte, 4*len(vec))
	for i, v := range vec {
		binary.LittleEndian.PutUint32(blob[i*4:], math.Float32bits(v))
	}
	return blob
}

// decodeEmbedding deserializes a little-endian byte blob to a float32 vector.
func decodeEmbedding(blob []byte) []float32 {
	if len(blob)%4 != 0 {
		return nil
	}
	vec := make([]float32, len(blob)/4)
	for i := range vec {
		vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(blob[i*4:]))
	}
	return vec
}

// parseDBTime parses a SQLite datetime value.
func parseDBTime(s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02 15:04:05", s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
	}
	return t, err
}
