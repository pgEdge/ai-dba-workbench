/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package conversations

import (
	"strings"
	"testing"
	"time"
)

func TestNewStore(t *testing.T) {
	// NewStore should accept a nil pool without panicking
	store := NewStore(nil)
	if store == nil {
		t.Fatal("Expected non-nil store")
	}
}

func TestGenerateID(t *testing.T) {
	id1 := generateID()
	time.Sleep(time.Nanosecond)
	id2 := generateID()

	if id1 == "" {
		t.Error("Expected non-empty ID")
	}
	if id1[:5] != "conv_" {
		t.Errorf("Expected ID to start with 'conv_', got %q", id1)
	}
	if id1 == id2 {
		t.Error("Expected unique IDs")
	}
}

func TestGenerateTitleFromMessages(t *testing.T) {
	tests := []struct {
		name     string
		messages []Message
		expected string
	}{
		{
			name:     "empty messages",
			messages: []Message{},
			expected: "New conversation",
		},
		{
			name: "only assistant message",
			messages: []Message{
				{Role: "assistant", Content: "Hello!"},
			},
			expected: "New conversation",
		},
		{
			name: "short user message",
			messages: []Message{
				{Role: "user", Content: "Hello"},
			},
			expected: "Hello",
		},
		{
			name: "long user message gets truncated",
			messages: []Message{
				{Role: "user", Content: "This is a very long message that should be truncated to a reasonable length for the title"},
			},
			expected: "This is a very long message that should be trun...",
		},
		{
			name: "empty user message content",
			messages: []Message{
				{Role: "user", Content: ""},
			},
			expected: "New conversation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generateTitle(tt.messages)
			if result != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestGenerateTitle_NonStringContent(t *testing.T) {
	// When Content is a non-string type (e.g., a slice of maps
	// representing tool use blocks), generateTitle should return
	// the default title.
	messages := []Message{
		{
			Role: "user",
			Content: []map[string]any{
				{"type": "tool_use", "name": "query"},
			},
		},
	}
	result := generateTitle(messages)
	if result != "New conversation" {
		t.Errorf("Expected 'New conversation', got %q", result)
	}
}

func TestGenerateTitle_MultipleMessages(t *testing.T) {
	// generateTitle should pick the first user message even when
	// assistant messages appear before it.
	messages := []Message{
		{Role: "assistant", Content: "Welcome!"},
		{Role: "assistant", Content: "How can I help?"},
		{Role: "user", Content: "Show me tables"},
		{Role: "user", Content: "Second question"},
	}
	result := generateTitle(messages)
	if result != "Show me tables" {
		t.Errorf("Expected 'Show me tables', got %q", result)
	}
}

func TestGenerateTitle_ExactlyFiftyChars(t *testing.T) {
	// Test boundary: exactly 50 characters should not be truncated
	msg := "12345678901234567890123456789012345678901234567890"
	if len(msg) != 50 {
		t.Fatalf("Test setup error: expected 50 chars, got %d", len(msg))
	}

	messages := []Message{
		{Role: "user", Content: msg},
	}
	result := generateTitle(messages)
	if result != msg {
		t.Errorf("Expected exactly 50 chars preserved, got %q (len=%d)", result, len(result))
	}
}

func TestGenerateTitle_FiftyOneChars(t *testing.T) {
	// Test boundary: 51 characters should be truncated
	msg := "123456789012345678901234567890123456789012345678901"
	if len(msg) != 51 {
		t.Fatalf("Test setup error: expected 51 chars, got %d", len(msg))
	}

	messages := []Message{
		{Role: "user", Content: msg},
	}
	result := generateTitle(messages)

	// Should be 47 chars + "..."
	expected := "12345678901234567890123456789012345678901234567..."
	if result != expected {
		t.Errorf("Expected truncated title %q, got %q", expected, result)
	}
}

func TestMessageStruct(t *testing.T) {
	msg := Message{
		Role:      "user",
		Content:   "test content",
		Timestamp: "2025-01-01T00:00:00Z",
		Provider:  "anthropic",
		Model:     "claude-3",
		IsError:   false,
	}

	if msg.Role != "user" {
		t.Errorf("Expected Role 'user', got %q", msg.Role)
	}
	if msg.Content != "test content" {
		t.Errorf("Expected Content 'test content', got %v", msg.Content)
	}
	if msg.Timestamp != "2025-01-01T00:00:00Z" {
		t.Errorf("Expected Timestamp, got %q", msg.Timestamp)
	}
	if msg.Provider != "anthropic" {
		t.Errorf("Expected Provider 'anthropic', got %q", msg.Provider)
	}
	if msg.Model != "claude-3" {
		t.Errorf("Expected Model 'claude-3', got %q", msg.Model)
	}
	if msg.IsError {
		t.Error("Expected IsError to be false")
	}
}

func TestConversationStruct(t *testing.T) {
	now := time.Now().UTC()
	conv := Conversation{
		ID:         "conv_123",
		Username:   "testuser",
		Title:      "Test Title",
		Provider:   "openai",
		Model:      "gpt-4",
		Connection: "conn_1",
		Messages:   []Message{{Role: "user", Content: "hello"}},
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	if conv.ID != "conv_123" {
		t.Errorf("Expected ID 'conv_123', got %q", conv.ID)
	}
	if conv.Username != "testuser" {
		t.Errorf("Expected Username 'testuser', got %q", conv.Username)
	}
	if conv.Title != "Test Title" {
		t.Errorf("Expected Title 'Test Title', got %q", conv.Title)
	}
	if conv.Provider != "openai" {
		t.Errorf("Expected Provider 'openai', got %q", conv.Provider)
	}
	if conv.Model != "gpt-4" {
		t.Errorf("Expected Model 'gpt-4', got %q", conv.Model)
	}
	if conv.Connection != "conn_1" {
		t.Errorf("Expected Connection 'conn_1', got %q", conv.Connection)
	}
	if len(conv.Messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(conv.Messages))
	}
}

func TestConversationSummaryStruct(t *testing.T) {
	now := time.Now().UTC()
	summary := ConversationSummary{
		ID:         "conv_456",
		Title:      "Summary Title",
		Connection: "conn_2",
		CreatedAt:  now,
		UpdatedAt:  now,
		Preview:    "This is a preview...",
	}

	if summary.ID != "conv_456" {
		t.Errorf("Expected ID 'conv_456', got %q", summary.ID)
	}
	if summary.Title != "Summary Title" {
		t.Errorf("Expected Title 'Summary Title', got %q", summary.Title)
	}
	if summary.Connection != "conn_2" {
		t.Errorf("Expected Connection 'conn_2', got %q", summary.Connection)
	}
	if summary.Preview != "This is a preview..." {
		t.Errorf("Expected Preview, got %q", summary.Preview)
	}
}

func TestGenerateID_Format(t *testing.T) {
	id := generateID()

	// Verify prefix
	if !strings.HasPrefix(id, "conv_") {
		t.Errorf("ID should start with 'conv_', got %q", id)
	}

	// Verify it's not just the prefix
	if len(id) <= 5 {
		t.Errorf("ID should be longer than just prefix, got %q", id)
	}
}

func TestGenerateID_Uniqueness(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := generateID()
		if ids[id] {
			t.Errorf("Duplicate ID generated: %q", id)
		}
		ids[id] = true
		time.Sleep(time.Nanosecond)
	}
}
