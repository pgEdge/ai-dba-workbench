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
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/pgedge/ai-workbench/server/internal/mcp"
)

// getBriefDescription extracts the first line or sentence from a description
func getBriefDescription(desc string) string {
	// Split by newlines and take first non-empty line
	lines := strings.Split(desc, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			// If line ends with period, return it
			if strings.HasSuffix(line, ".") {
				return line
			}
			// Otherwise, find first sentence (period followed by space or end)
			if idx := strings.Index(line, ". "); idx != -1 {
				return line[:idx+1]
			}
			// No period found, return the whole line
			return line
		}
	}
	return desc
}

// estimateTokens estimates the number of tokens in a string.
// Uses a rough heuristic of ~3.5 characters per token.
func estimateTokens(text string) int {
	if text == "" {
		return 0
	}
	// Rough heuristic: ~4 characters per token for English, ~3 for code/JSON
	// Use 3.5 as a middle ground to be conservative
	return (len(text) + 2) / 3 // Rounds up, slightly more conservative than /3.5
}

// estimateTotalTokens estimates the total tokens in a message array.
func estimateTotalTokens(messages []Message) int {
	total := 0
	for _, msg := range messages {
		switch content := msg.Content.(type) {
		case string:
			total += estimateTokens(content)
		case []interface{}:
			// Handle tool_use and tool_result arrays
			for _, item := range content {
				if m, ok := item.(map[string]interface{}); ok {
					if text, ok := m["text"].(string); ok {
						total += estimateTokens(text)
					}
					if input, ok := m["input"]; ok {
						if jsonBytes, err := json.Marshal(input); err == nil {
							total += estimateTokens(string(jsonBytes))
						}
					}
					if c, ok := m["content"]; ok {
						if text, ok := c.(string); ok {
							total += estimateTokens(text)
						}
					}
				}
			}
		case []ToolResult:
			for _, tr := range content {
				switch c := tr.Content.(type) {
				case []mcp.ContentItem:
					for _, item := range c {
						total += estimateTokens(item.Text)
					}
				case string:
					total += estimateTokens(c)
				}
			}
		}
		// Add overhead for message structure (~10 tokens per message)
		total += 10
	}
	return total
}

// compactMessages reduces the message history to prevent token overflow.
// It tries to use the server-side smart compaction if available in HTTP mode,
// falling back to local basic compaction if needed.
func (c *Client) compactMessages(messages []Message) []Message {
	const maxRecentMessages = 10
	const maxTokens = 100000
	// Compact if estimated tokens exceed this threshold.
	// Note: Anthropic rate limits are typically 30k-60k input tokens/minute cumulative.
	// Setting lower allows multiple requests within the rate limit window.
	const tokenCompactionThreshold = 15000

	const minMessagesForCompaction = 15 // Don't compact unless we have at least 15 messages
	const minSavingsThreshold = 5       // Only compact if we can save at least 5 messages

	// Estimate total tokens in the conversation
	estimatedTokens := estimateTotalTokens(messages)

	// Check if we should compact based on token count OR message count
	shouldCompactByTokens := estimatedTokens > tokenCompactionThreshold
	shouldCompactByMessages := len(messages) >= minMessagesForCompaction

	// If neither threshold is met, skip compaction
	if !shouldCompactByTokens && !shouldCompactByMessages {
		return messages
	}

	// Log why we're compacting (for debugging)
	if c.config.UI.Debug {
		if shouldCompactByTokens {
			fmt.Fprintf(os.Stderr, "[DEBUG] Compaction triggered by token count: ~%d tokens (threshold: %d)\n",
				estimatedTokens, tokenCompactionThreshold)
		} else {
			fmt.Fprintf(os.Stderr, "[DEBUG] Compaction triggered by message count: %d messages (threshold: %d)\n",
				len(messages), minMessagesForCompaction)
		}
	}

	// Estimate if compaction would be worthwhile (only for message-based trigger)
	// With recentWindow=10 and keepAnchors=true, we keep at least: 1 (first) + 10 (recent) = 11
	// So we need at least 11 + minSavingsThreshold messages to make it worthwhile
	// For token-based trigger, always proceed since we need to reduce tokens
	if !shouldCompactByTokens && len(messages) < (11+minSavingsThreshold) {
		return messages
	}

	// Try server-side smart compaction if in HTTP mode
	if compacted, ok := c.tryServerCompaction(messages, maxTokens, maxRecentMessages, minSavingsThreshold); ok {
		return compacted
	}

	// Fall back to local basic compaction
	localCompacted := c.localCompactMessages(messages, maxRecentMessages)
	messagesSaved := len(messages) - len(localCompacted)

	// Only use local compaction if it actually saves enough messages
	if messagesSaved < minSavingsThreshold {
		if c.config.UI.Debug {
			fmt.Fprintf(os.Stderr, "[DEBUG] Local compaction skipped - only saved %d messages (threshold: %d)\n",
				messagesSaved, minSavingsThreshold)
		}
		return messages
	}

	return localCompacted
}

// tryServerCompaction attempts to use the server's smart compaction endpoint.
func (c *Client) tryServerCompaction(messages []Message, maxTokens, recentWindow, minSavingsThreshold int) ([]Message, bool) {
	// Only available in HTTP mode
	httpClient, ok := c.mcp.(*httpClient)
	if !ok {
		return nil, false
	}

	// Build compaction request
	reqBody := CompactionRequest{
		Messages:     messages,
		MaxTokens:    maxTokens,
		RecentWindow: recentWindow,
		KeepAnchors:  true,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		if c.config.UI.Debug {
			fmt.Fprintf(os.Stderr, "[DEBUG] Failed to marshal compaction request: %v\n", err)
		}
		return nil, false
	}

	// Call the compaction endpoint
	req, err := http.NewRequest("POST", httpClient.url+"/api/v1/chat/compact", bytes.NewBuffer(jsonData))
	if err != nil {
		if c.config.UI.Debug {
			fmt.Fprintf(os.Stderr, "[DEBUG] Failed to create compaction request: %v\n", err)
		}
		return nil, false
	}

	req.Header.Set("Content-Type", "application/json")
	if httpClient.token != "" {
		req.Header.Set("Authorization", "Bearer "+httpClient.token)
	}

	resp, err := httpClient.client.Do(req)
	if err != nil {
		if c.config.UI.Debug {
			fmt.Fprintf(os.Stderr, "[DEBUG] Compaction request failed: %v\n", err)
		}
		return nil, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if c.config.UI.Debug {
			fmt.Fprintf(os.Stderr, "[DEBUG] Compaction returned status %d\n", resp.StatusCode)
		}
		return nil, false
	}

	// Parse response
	var compactResp CompactionResponse
	if err := json.NewDecoder(resp.Body).Decode(&compactResp); err != nil {
		if c.config.UI.Debug {
			fmt.Fprintf(os.Stderr, "[DEBUG] Failed to decode compaction response: %v\n", err)
		}
		return nil, false
	}

	// Check if compaction actually saved enough messages
	info := compactResp.CompactionInfo
	messagesSaved := info.OriginalCount - info.CompactedCount
	if messagesSaved < minSavingsThreshold {
		if c.config.UI.Debug {
			fmt.Fprintf(os.Stderr, "[DEBUG] Server compaction skipped - only saved %d messages (threshold: %d)\n",
				messagesSaved, minSavingsThreshold)
		}
		return nil, false
	}

	// Show compaction status to user (only when actually using it)
	fmt.Fprintf(os.Stderr, "Compacting chat history...\n")

	if c.config.UI.Debug {
		fmt.Fprintf(os.Stderr, "[DEBUG] Server compaction: %d -> %d messages (dropped %d, saved %d tokens, ratio %.2f)\n",
			info.OriginalCount, info.CompactedCount, info.DroppedCount,
			info.TokensSaved, info.CompressionRatio)
	}

	return compactResp.Messages, true
}

// localCompactMessages performs basic local compaction.
// Strategy: Keep the first user message and the last N messages.
// This preserves the original query context while maintaining recent conversation flow.
// IMPORTANT: Ensures tool_use/tool_result message pairs are kept together to avoid
// API errors from orphaned tool references.
func (c *Client) localCompactMessages(messages []Message, maxRecentMessages int) []Message {
	compacted := make([]Message, 0, maxRecentMessages+1)

	// Keep the first user message (original query)
	if len(messages) > 0 && messages[0].Role == "user" {
		compacted = append(compacted, messages[0])
	}

	// Keep the last N messages
	startIdx := len(messages) - maxRecentMessages
	if startIdx < 1 {
		startIdx = 1 // Skip first message since we already added it
	}

	// Ensure we don't break tool_use/tool_result pairs
	// If the first message we're keeping contains tool_results, we must also
	// keep the preceding assistant message that contains the tool_use blocks
	startIdx = c.adjustStartForToolPairs(messages, startIdx)

	compacted = append(compacted, messages[startIdx:]...)

	if c.config.UI.Debug {
		fmt.Fprintf(os.Stderr, "[DEBUG] Local compaction: %d -> %d (kept first + last %d)\n",
			len(messages), len(compacted), maxRecentMessages)
	}

	return compacted
}

// adjustStartForToolPairs adjusts the start index to ensure tool_use/tool_result
// message pairs are kept together. If the message at startIdx contains tool_results,
// we need to include the preceding assistant message with tool_use blocks.
func (c *Client) adjustStartForToolPairs(messages []Message, startIdx int) int {
	if startIdx <= 1 || startIdx >= len(messages) {
		return startIdx
	}

	// Check if the message at startIdx is a user message with tool_results
	msg := messages[startIdx]
	if msg.Role != "user" {
		return startIdx
	}

	// Check if this message contains tool_result blocks
	if c.hasToolResults(msg) {
		// Include the preceding assistant message (which should have tool_use)
		if startIdx > 1 {
			startIdx--
		}
	}

	return startIdx
}

// hasToolResults checks if a message contains tool_result blocks.
func (c *Client) hasToolResults(msg Message) bool {
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
