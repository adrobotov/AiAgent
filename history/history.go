package history

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

// Message represents a chat message for history storage.
type Message struct {
	Role    string
	Name    string
	Content string
}

// Store manages conversation history persistence.
type Store struct {
	db *sql.DB
}

// New creates a new history store.
func New() (*Store, error) {
	dir := ConfigDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create history dir: %w", err)
	}

	dbPath := filepath.Join(dir, "history.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	store := &Store{db: db}
	if err := store.initDB(); err != nil {
		return nil, err
	}

	return store, nil
}

// initDB creates the required tables.
func (s *Store) initDB() error {
	query := `
	CREATE TABLE IF NOT EXISTS conversations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		message_count INTEGER DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		conversation_id INTEGER,
		role TEXT NOT NULL,
		content TEXT NOT NULL,
		name TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (conversation_id) REFERENCES conversations(id)
	);

	CREATE INDEX IF NOT EXISTS idx_messages_conversation ON messages(conversation_id);
	`

	_, err := s.db.Exec(query)
	return err
}

// NewConversation creates a new conversation and returns its ID.
func (s *Store) NewConversation() (int64, error) {
	res, err := s.db.Exec("INSERT INTO conversations (message_count) VALUES (0)")
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// SaveMessage saves a message to the current conversation.
func (s *Store) SaveMessage(convID int64, msg Message) error {
	_, err := s.db.Exec(
		"INSERT INTO messages (conversation_id, role, content, name) VALUES (?, ?, ?, ?)",
		convID, msg.Role, msg.Content, msg.Name,
	)
	if err != nil {
		return err
	}

	// Update message count
	_, err = s.db.Exec("UPDATE conversations SET message_count = message_count + 1 WHERE id = ?", convID)
	return err
}

// GetMessages retrieves messages for a conversation.
func (s *Store) GetMessages(convID int64, limit int) ([]Message, error) {
	rows, err := s.db.Query(
		"SELECT role, content, name FROM messages WHERE conversation_id = ? ORDER BY id DESC LIMIT ?",
		convID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var msg Message
		if err := rows.Scan(&msg.Role, &msg.Content, &msg.Name); err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}
	return messages, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// ConfigDir returns the directory for history files.
func ConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".agent")
}
