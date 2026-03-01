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
