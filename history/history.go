package history

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	// Pure-Go SQLite driver — enables CGO_ENABLED=0 cross-compilation.
	_ "modernc.org/sqlite"
)

// Message represents a chat message for history storage.
type Message struct {
	Role    string
	Name    string
	Content string
}

// Conversation represents a conversation with metadata.
type Conversation struct {
	ID           int64
	Name         string
	CreatedAt    time.Time
	MessageCount int
}

// Store manages conversation history persistence.
type Store struct {
	db          *sql.DB
	currentConv int64
}

// New creates a new history store using the default config directory.
func New() (*Store, error) {
	return NewWithPath("")
}

// NewWithPath creates a new history store with an explicit database path.
// If path is empty, uses the default config directory.
func NewWithPath(path string) (*Store, error) {
	dbPath := path
	if dbPath == "" {
		dbPath = DefaultDBPath()
	}

	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create history dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	store := &Store{db: db, currentConv: 0}
	if err := store.initDB(); err != nil {
		return nil, err
	}

	return store, nil
}

// DefaultDBPath returns the default database path in the user's home directory.
func DefaultDBPath() string {
	dir := ConfigDir()
	return filepath.Join(dir, "history.db")
}

// initDB creates the required tables and runs migrations.
func (s *Store) initDB() error {
	query := `
	CREATE TABLE IF NOT EXISTS conversations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT DEFAULT '',
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
		FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_messages_conversation ON messages(conversation_id);
	`

	_, err := s.db.Exec(query)
	if err != nil {
		return fmt.Errorf("create tables: %w", err)
	}

	// Migration: add name column if it doesn't exist.
	// SQLite returns an error when the column already exists; ignore it.
	_, _ = s.db.Exec("ALTER TABLE conversations ADD COLUMN name TEXT DEFAULT ''")

	// Long-term memory table
	if err := s.initMemoryDB(); err != nil {
		return err
	}

	return nil
}

// NewConversation creates a new conversation and returns its ID.
func (s *Store) NewConversation() (int64, error) {
	res, err := s.db.Exec(
		"INSERT INTO conversations (name, message_count) VALUES (?, 0)",
		"",
	)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	s.currentConv = id
	return id, nil
}

// SetCurrentConversation sets the active conversation ID.
func (s *Store) SetCurrentConversation(id int64) {
	s.currentConv = id
}

// CurrentConversation returns the active conversation ID.
func (s *Store) CurrentConversation() int64 {
	return s.currentConv
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
		"SELECT role, content, name FROM messages WHERE conversation_id = ? ORDER BY id ASC LIMIT ?",
		convID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

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

// ListConversations returns all conversations ordered by creation date (newest first).
func (s *Store) ListConversations() ([]Conversation, error) {
	rows, err := s.db.Query(
		"SELECT id, COALESCE(name, ''), created_at, message_count FROM conversations ORDER BY created_at DESC",
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var conversations []Conversation
	for rows.Next() {
		var c Conversation
		var createdAt string
		if err := rows.Scan(&c.ID, &c.Name, &createdAt, &c.MessageCount); err != nil {
			return nil, err
		}
		c.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		if c.CreatedAt.IsZero() {
			c.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		}
		conversations = append(conversations, c)
	}
	return conversations, nil
}

// GetConversation returns a single conversation by ID.
func (s *Store) GetConversation(id int64) (*Conversation, error) {
	var c Conversation
	var createdAt string
	err := s.db.QueryRow(
		"SELECT id, COALESCE(name, ''), created_at, message_count FROM conversations WHERE id = ?",
		id,
	).Scan(&c.ID, &c.Name, &createdAt, &c.MessageCount)
	if err != nil {
		return nil, err
	}
	c.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	if c.CreatedAt.IsZero() {
		c.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	}
	return &c, nil
}

// RenameConversation renames a conversation.
func (s *Store) RenameConversation(id int64, name string) error {
	_, err := s.db.Exec("UPDATE conversations SET name = ? WHERE id = ?", name, id)
	return err
}

// DeleteConversation deletes a conversation and all its messages.
func (s *Store) DeleteConversation(id int64) error {
	_, err := s.db.Exec("DELETE FROM conversations WHERE id = ?", id)
	return err
}

// DeleteAllConversations deletes all conversations and their messages.
func (s *Store) DeleteAllConversations() error {
	_, err := s.db.Exec("DELETE FROM messages")
	if err != nil {
		return err
	}
	_, err = s.db.Exec("DELETE FROM conversations")
	if err != nil {
		return err
	}
	// Reset autoincrement (best-effort).
	_, _ = s.db.Exec("DELETE FROM sqlite_sequence WHERE name='conversations'")
	_, _ = s.db.Exec("DELETE FROM sqlite_sequence WHERE name='messages'")
	return nil
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
