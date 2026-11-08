package history

import (
	"testing"
)

func setupTestStore(t *testing.T) *Store {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/history.db"

	store, err := NewWithPath(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestNewConversation(t *testing.T) {
	store := setupTestStore(t)

	id, err := store.NewConversation()
	if err != nil {
		t.Fatalf("failed to create conversation: %v", err)
	}
	if id <= 0 {
		t.Errorf("expected positive conversation ID, got %d", id)
	}
}

func TestSaveAndGetMessages(t *testing.T) {
	store := setupTestStore(t)

	convID, _ := store.NewConversation()

	// Save messages
	store.SaveMessage(convID, Message{Role: "user", Content: "Hello"})
	store.SaveMessage(convID, Message{Role: "assistant", Content: "Hi there!"})

	// Retrieve messages
	messages, err := store.GetMessages(convID, 10)
	if err != nil {
		t.Fatalf("failed to get messages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if messages[0].Content != "Hello" {
		t.Errorf("expected 'Hello', got '%s'", messages[0].Content)
	}
	if messages[1].Content != "Hi there!" {
		t.Errorf("expected 'Hi there!', got '%s'", messages[1].Content)
	}
}

func TestListConversations(t *testing.T) {
	store := setupTestStore(t)

	// Create multiple conversations
	for i := 0; i < 3; i++ {
		_, err := store.NewConversation()
		if err != nil {
			t.Fatalf("failed to create conversation: %v", err)
		}
	}

	convs, err := store.ListConversations()
	if err != nil {
		t.Fatalf("failed to list conversations: %v", err)
	}
	if len(convs) != 3 {
		t.Errorf("expected 3 conversations, got %d", len(convs))
	}
}

func TestRenameConversation(t *testing.T) {
	store := setupTestStore(t)

	convID, _ := store.NewConversation()

	// Rename
	err := store.RenameConversation(convID, "Test Conversation")
	if err != nil {
		t.Fatalf("failed to rename conversation: %v", err)
	}

	// Verify
	conv, err := store.GetConversation(convID)
	if err != nil {
		t.Fatalf("failed to get conversation: %v", err)
	}
	if conv.Name != "Test Conversation" {
		t.Errorf("expected 'Test Conversation', got '%s'", conv.Name)
	}
}

func TestDeleteConversation(t *testing.T) {
	store := setupTestStore(t)

	convID, _ := store.NewConversation()

	// Delete
	err := store.DeleteConversation(convID)
	if err != nil {
		t.Fatalf("failed to delete conversation: %v", err)
	}

	// Verify it's gone
	_, err = store.GetConversation(convID)
	if err == nil {
		t.Error("expected error for deleted conversation, got nil")
	}

	convs, _ := store.ListConversations()
	if len(convs) != 0 {
		t.Errorf("expected 0 conversations after delete, got %d", len(convs))
	}
}

func TestCurrentConversation(t *testing.T) {
	store := setupTestStore(t)

	// Create first conversation
	convID1, _ := store.NewConversation()
	if store.CurrentConversation() != convID1 {
		t.Errorf("expected current conv to be %d, got %d", convID1, store.CurrentConversation())
	}

	// Create second conversation
	convID2, _ := store.NewConversation()
	if store.CurrentConversation() != convID2 {
		t.Errorf("expected current conv to be %d, got %d", convID2, store.CurrentConversation())
	}

	// Set manually
	store.SetCurrentConversation(convID1)
	if store.CurrentConversation() != convID1 {
		t.Errorf("expected current conv to be %d after SetCurrentConversation", convID1)
	}
}

func TestMessageCount(t *testing.T) {
	store := setupTestStore(t)

	convID, _ := store.NewConversation()

	// Save some messages
	store.SaveMessage(convID, Message{Role: "user", Content: "Msg 1"})
	store.SaveMessage(convID, Message{Role: "assistant", Content: "Reply 1"})
	store.SaveMessage(convID, Message{Role: "user", Content: "Msg 2"})

	// Check message count
	conv, err := store.GetConversation(convID)
	if err != nil {
		t.Fatalf("failed to get conversation: %v", err)
	}
	if conv.MessageCount != 3 {
		t.Errorf("expected 3 messages, got %d", conv.MessageCount)
	}
}

func TestDeleteAllConversations(t *testing.T) {
	store := setupTestStore(t)

	// Create some conversations
	for i := 0; i < 3; i++ {
		id, _ := store.NewConversation()
		store.SaveMessage(id, Message{Role: "user", Content: "test"})
	}

	// Delete all
	err := store.DeleteAllConversations()
	if err != nil {
		t.Fatalf("failed to delete all conversations: %v", err)
	}

	convs, _ := store.ListConversations()
	if len(convs) != 0 {
		t.Errorf("expected 0 conversations, got %d", len(convs))
	}
}
