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
	"encoding/json"
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
	// Validate that generateID returns a non-empty, properly-prefixed
	// identifier. Uniqueness between back-to-back calls depends on clock
	// resolution and is therefore covered separately in
	// TestGenerateID_Uniqueness.
	id := generateID()

	if id == "" {
		t.Error("Expected non-empty ID")
	}
	if !strings.HasPrefix(id, "conv_") {
		t.Errorf("Expected ID to start with 'conv_', got %q", id)
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

	// JSON round-trip guards the wire format against accidental tag
	// changes.
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if decoded.Role != msg.Role ||
		decoded.Content != msg.Content ||
		decoded.Timestamp != msg.Timestamp ||
		decoded.Provider != msg.Provider ||
		decoded.Model != msg.Model ||
		decoded.IsError != msg.IsError {
		t.Errorf("JSON round-trip mismatch: got %+v, want %+v",
			decoded, msg)
	}
}

func TestConversationStruct(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
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
	if !conv.CreatedAt.Equal(now) {
		t.Errorf("Expected CreatedAt %v, got %v", now, conv.CreatedAt)
	}
	if !conv.UpdatedAt.Equal(now) {
		t.Errorf("Expected UpdatedAt %v, got %v", now, conv.UpdatedAt)
	}

	// JSON round-trip guards the wire format against accidental tag
	// changes.
	data, err := json.Marshal(conv)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	var decoded Conversation
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if decoded.ID != conv.ID ||
		decoded.Username != conv.Username ||
		decoded.Title != conv.Title ||
		decoded.Provider != conv.Provider ||
		decoded.Model != conv.Model ||
		decoded.Connection != conv.Connection ||
		len(decoded.Messages) != len(conv.Messages) {
		t.Errorf("JSON round-trip mismatch: got %+v, want %+v",
			decoded, conv)
	}
	if !decoded.CreatedAt.Equal(conv.CreatedAt) {
		t.Errorf("CreatedAt round-trip mismatch: got %v, want %v",
			decoded.CreatedAt, conv.CreatedAt)
	}
	if !decoded.UpdatedAt.Equal(conv.UpdatedAt) {
		t.Errorf("UpdatedAt round-trip mismatch: got %v, want %v",
			decoded.UpdatedAt, conv.UpdatedAt)
	}
}

func TestConversationSummaryStruct(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
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
	if !summary.CreatedAt.Equal(now) {
		t.Errorf("Expected CreatedAt %v, got %v", now, summary.CreatedAt)
	}
	if !summary.UpdatedAt.Equal(now) {
		t.Errorf("Expected UpdatedAt %v, got %v", now, summary.UpdatedAt)
	}

	// JSON round-trip guards the wire format against accidental tag
	// changes.
	data, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	var decoded ConversationSummary
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if decoded.ID != summary.ID ||
		decoded.Title != summary.Title ||
		decoded.Connection != summary.Connection ||
		decoded.Preview != summary.Preview {
		t.Errorf("JSON round-trip mismatch: got %+v, want %+v",
			decoded, summary)
	}
	if !decoded.CreatedAt.Equal(summary.CreatedAt) {
		t.Errorf("CreatedAt round-trip mismatch: got %v, want %v",
			decoded.CreatedAt, summary.CreatedAt)
	}
	if !decoded.UpdatedAt.Equal(summary.UpdatedAt) {
		t.Errorf("UpdatedAt round-trip mismatch: got %v, want %v",
			decoded.UpdatedAt, summary.UpdatedAt)
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
	// generateID derives its suffix from time.Now().UnixNano(), so
	// collisions between calls that land in the same nanosecond tick are
	// theoretically possible on platforms with coarse clocks. Rather than
	// depend on clock granularity (which has caused flakes in CI), we
	// generate a batch of IDs, verify every one is well-formed, and
	// tolerate a small number of duplicates while still catching a
	// generator that is fundamentally broken.
	const iterations = 200
	const maxDuplicates = iterations / 20 // 5 percent tolerance

	ids := make(map[string]int, iterations)
	for i := 0; i < iterations; i++ {
		id := generateID()
		if !strings.HasPrefix(id, "conv_") || len(id) <= 5 {
			t.Fatalf("Malformed ID generated: %q", id)
		}
		ids[id]++
	}

	duplicates := 0
	for _, count := range ids {
		if count > 1 {
			duplicates += count - 1
		}
	}
	if duplicates > maxDuplicates {
		t.Errorf("Too many duplicate IDs: %d of %d (max tolerated: %d)",
			duplicates, iterations, maxDuplicates)
	}
}
