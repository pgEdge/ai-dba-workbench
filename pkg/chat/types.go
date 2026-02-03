/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package chat

import (
	"time"

	"github.com/pgedge/ai-workbench/pkg/mcp"
)

// Message represents a chat message
type Message struct {
	Role         string                 `json:"role"`
	Content      interface{}            `json:"content"`
	CacheControl map[string]interface{} `json:"cache_control,omitempty"`
}

// ToolUse represents a tool invocation in a message
type ToolUse struct {
	Type  string                 `json:"type"`
	ID    string                 `json:"id"`
	Name  string                 `json:"name"`
	Input map[string]interface{} `json:"input"`
}

// TextContent represents text content in a message
type TextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ToolResult represents the result of a tool execution
type ToolResult struct {
	Type      string      `json:"type"`
	ToolUseID string      `json:"tool_use_id"`
	Content   interface{} `json:"content"`
	IsError   bool        `json:"is_error,omitempty"`
}

// LLMResponse represents a response from the LLM
type LLMResponse struct {
	Content    []interface{} // Can be TextContent or ToolUse
	StopReason string
	TokenUsage *TokenUsage `json:"token_usage,omitempty"`
}

// TokenUsage holds token usage information for debug purposes
type TokenUsage struct {
	Provider               string  `json:"provider"`
	PromptTokens           int     `json:"prompt_tokens,omitempty"`
	CompletionTokens       int     `json:"completion_tokens,omitempty"`
	TotalTokens            int     `json:"total_tokens,omitempty"`
	CacheCreationTokens    int     `json:"cache_creation_tokens,omitempty"`
	CacheReadTokens        int     `json:"cache_read_tokens,omitempty"`
	CacheSavingsPercentage float64 `json:"cache_savings_percentage,omitempty"`
}

// ConversationSummary provides a lightweight view for listing
type ConversationSummary struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Connection string    `json:"connection,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Preview    string    `json:"preview"`
}

// Conversation represents a stored conversation
type Conversation struct {
	ID         string    `json:"id"`
	Username   string    `json:"username"`
	Title      string    `json:"title"`
	Provider   string    `json:"provider,omitempty"`
	Model      string    `json:"model,omitempty"`
	Connection string    `json:"connection,omitempty"`
	Messages   []Message `json:"messages"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// CompactionRequest represents a request to compact chat history.
type CompactionRequest struct {
	Messages     []Message `json:"messages"`
	MaxTokens    int       `json:"max_tokens,omitempty"`
	RecentWindow int       `json:"recent_window,omitempty"`
	KeepAnchors  bool      `json:"keep_anchors"`
}

// CompactionResponse contains the compacted messages and statistics.
type CompactionResponse struct {
	Messages       []Message      `json:"messages"`
	TokenEstimate  int            `json:"token_estimate"`
	CompactionInfo CompactionInfo `json:"compaction_info"`
}

// CompactionInfo provides statistics about the compaction operation.
type CompactionInfo struct {
	OriginalCount    int     `json:"original_count"`
	CompactedCount   int     `json:"compacted_count"`
	DroppedCount     int     `json:"dropped_count"`
	TokensSaved      int     `json:"tokens_saved"`
	CompressionRatio float64 `json:"compression_ratio"`
}

// ListResponse represents the response from list endpoint
type ListResponse struct {
	Conversations []ConversationSummary `json:"conversations"`
}

// CreateConversationRequest represents a request to create a conversation
type CreateConversationRequest struct {
	Provider   string    `json:"provider"`
	Model      string    `json:"model"`
	Connection string    `json:"connection"`
	Messages   []Message `json:"messages"`
}

// RenameConversationRequest represents a request to rename a conversation
type RenameConversationRequest struct {
	Title string `json:"title"`
}

// HasToolResults checks if a message contains tool_result blocks.
func HasToolResults(msg Message) bool {
	content, ok := msg.Content.([]ToolResult)
	if ok && len(content) > 0 {
		return true
	}

	// Also check for []interface{} format (from JSON unmarshaling)
	if contentSlice, ok := msg.Content.([]interface{}); ok {
		for _, item := range contentSlice {
			if itemMap, ok := item.(map[string]interface{}); ok {
				if itemType, ok := itemMap["type"].(string); ok && itemType == "tool_result" {
					return true
				}
			}
		}
	}

	return false
}

// EstimateTokens estimates the number of tokens in a string.
// Uses a rough heuristic of ~3.5 characters per token.
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	// Rough heuristic: ~4 characters per token for English, ~3 for code/JSON
	// Use 3.5 as a middle ground to be conservative
	return (len(text) + 2) / 3 // Rounds up, slightly more conservative than /3.5
}

// EstimateTotalTokens estimates the total tokens in a message array.
func EstimateTotalTokens(messages []Message) int {
	total := 0
	for _, msg := range messages {
		switch content := msg.Content.(type) {
		case string:
			total += EstimateTokens(content)
		case []interface{}:
			// Handle tool_use and tool_result arrays
			for _, item := range content {
				if m, ok := item.(map[string]interface{}); ok {
					if text, ok := m["text"].(string); ok {
						total += EstimateTokens(text)
					}
					if input, ok := m["input"]; ok {
						// Note: Using json.Marshal here would create import cycle concerns
						// so we estimate based on the type
						if inputMap, ok := input.(map[string]interface{}); ok {
							total += len(inputMap) * 20 // Rough estimate per field
						}
					}
					if c, ok := m["content"]; ok {
						if text, ok := c.(string); ok {
							total += EstimateTokens(text)
						}
					}
				}
			}
		case []ToolResult:
			for _, tr := range content {
				switch c := tr.Content.(type) {
				case []mcp.ContentItem:
					for _, item := range c {
						total += EstimateTokens(item.Text)
					}
				case string:
					total += EstimateTokens(c)
				}
			}
		}
		// Add overhead for message structure (~10 tokens per message)
		total += 10
	}
	return total
}

// GetBriefDescription extracts the first line or sentence from a description
func GetBriefDescription(desc string) string {
	// Split by newlines and take first non-empty line
	lines := splitLines(desc)
	for _, line := range lines {
		line = trimSpace(line)
		if line != "" {
			// If line ends with period, return it
			if hasSuffix(line, ".") {
				return line
			}
			// Otherwise, find first sentence (period followed by space or end)
			if idx := indexOf(line, ". "); idx != -1 {
				return line[:idx+1]
			}
			// No period found, return the whole line
			return line
		}
	}
	return desc
}

// Helper functions to avoid importing strings package
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
