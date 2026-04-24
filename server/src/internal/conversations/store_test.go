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
	"errors"
	"strings"
	"testing"
	"time"
)

// failingReader is an io.Reader that always returns an error. It drives
// the crypto/rand failure branch of generateIDFrom without mocking the
// global rand.Reader, keeping the test hermetic.
type failingReader struct{}

func (failingReader) Read(_ []byte) (int, error) {
	return 0, errors.New("simulated randomness failure")
}

func TestNewStore(t *testing.T) {
	// NewStore should accept a nil pool without panicking
	store := NewStore(nil)
	if store == nil {
		t.Fatal("Expected non-nil store")
	}
}

func TestGenerateID(t *testing.T) {
	// Validate that generateID returns a non-empty, properly-prefixed
	// identifier. Back-to-back uniqueness is covered separately in
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

	// Verify the full shape: conv_<unix-nanos>_<16 hex chars>. The
	// random suffix is what guarantees uniqueness across calls that
	// fall inside a single nanosecond tick, so we assert its presence
	// and length explicitly. A fallback path that omits the suffix
	// exists for the (practically unreachable) case where crypto/rand
	// returns an error; this test exercises the normal path.
	parts := strings.Split(id, "_")
	if len(parts) != 3 {
		t.Fatalf("ID should have 3 underscore-separated parts, got %q", id)
	}
	if parts[0] != "conv" {
		t.Errorf("First part should be 'conv', got %q", parts[0])
	}
	if len(parts[1]) == 0 {
		t.Errorf("Timestamp part should be non-empty, got %q", id)
	}
	for _, r := range parts[1] {
		if r < '0' || r > '9' {
			t.Errorf("Timestamp part should be all digits, got %q", parts[1])
			break
		}
	}
	if len(parts[2]) != 16 {
		t.Errorf("Random suffix should be 16 hex chars (8 bytes), "+
			"got %q (length %d)", parts[2], len(parts[2]))
	}
	for _, r := range parts[2] {
		isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
		if !isHex {
			t.Errorf("Random suffix should be lowercase hex, got %q",
				parts[2])
			break
		}
	}
}

func TestGenerateIDFrom_RandomFailureFallsBackToTimestamp(t *testing.T) {
	// When the randomness source returns an error, generateIDFrom must
	// still produce a well-formed, non-empty ID. The fallback path drops
	// the random suffix and returns conv_<unix-nanos>; we assert the
	// shape here so future refactors cannot silently break the fallback.
	id := generateIDFrom(failingReader{})

	if !strings.HasPrefix(id, "conv_") {
		t.Errorf("Fallback ID should start with 'conv_', got %q", id)
	}
	parts := strings.Split(id, "_")
	if len(parts) != 2 {
		t.Fatalf("Fallback ID should have exactly 2 underscore-separated "+
			"parts (conv_<nanos>), got %q", id)
	}
	if parts[0] != "conv" {
		t.Errorf("First part should be 'conv', got %q", parts[0])
	}
	if len(parts[1]) == 0 {
		t.Errorf("Timestamp part should be non-empty, got %q", id)
	}
	for _, r := range parts[1] {
		if r < '0' || r > '9' {
			t.Errorf("Timestamp part should be all digits, got %q", parts[1])
			break
		}
	}
}

func TestGenerateIDFrom_UsesProvidedRandomness(t *testing.T) {
	// When the reader supplies known bytes, generateIDFrom must emit
	// them verbatim as the hex suffix. This locks down the encoding and
	// ensures the random source is actually consulted (rather than, say,
	// a hard-coded constant or the global rand.Reader).
	fixed := strings.NewReader("\x00\x11\x22\x33\x44\x55\x66\x77")
	id := generateIDFrom(fixed)

	if !strings.HasSuffix(id, "_0011223344556677") {
		t.Errorf("Expected ID to end with the hex of the supplied bytes, "+
			"got %q", id)
	}
}

func TestGenerateID_Uniqueness(t *testing.T) {
	// generateID combines a nanosecond timestamp with a 64-bit
	// cryptographically random suffix, so collisions in a tight loop
	// are astronomically unlikely (roughly 1 in 2^64 per pair). We
	// therefore require zero duplicates across a 200-iteration batch;
	// any collision here indicates a regression in the generator.
	const iterations = 200

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
	if duplicates != 0 {
		t.Errorf("Unexpected duplicate IDs: %d of %d (expected 0)",
			duplicates, iterations)
	}
}
