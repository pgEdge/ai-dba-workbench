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
	"io"
	"net/http"
	"os"

	pkgchat "github.com/pgedge/ai-workbench/pkg/chat"
)

// compactMessages reduces the message history to prevent token overflow.
// It tries to use the server-side smart compaction if available in HTTP mode,
// falling back to local basic compaction if needed.
func (c *Client) compactMessages(messages []Message) []Message {
	// Check whether compaction is needed
	shouldCompactByTokens, shouldCompactByMessages := pkgchat.ShouldCompact(messages)
	if !shouldCompactByTokens && !shouldCompactByMessages {
		return messages
	}

	// Log why we're compacting (for debugging)
	if c.config.UI.Debug {
		if shouldCompactByTokens {
			estimatedTokens := pkgchat.EstimateTotalTokens(messages)
			fmt.Fprintf(os.Stderr, "[DEBUG] Compaction triggered by token count: ~%d tokens (threshold: %d)\n",
				estimatedTokens, pkgchat.CompactionTokenThreshold)
		} else {
			fmt.Fprintf(os.Stderr, "[DEBUG] Compaction triggered by message count: %d messages (threshold: %d)\n",
				len(messages), pkgchat.CompactionMinMessages)
		}
	}

	// Estimate if compaction would be worthwhile (only for message-based trigger)
	if !shouldCompactByTokens && len(messages) < (11+pkgchat.CompactionMinSavings) {
		return messages
	}

	// Try server-side smart compaction if in HTTP mode
	if compacted, ok := c.tryServerCompaction(messages, pkgchat.CompactionMaxTokens, pkgchat.CompactionMaxRecentMessages, pkgchat.CompactionMinSavings); ok {
		return compacted
	}

	// Fall back to local basic compaction
	var debugWriter io.Writer
	if c.config.UI.Debug {
		debugWriter = os.Stderr
	}
	localCompacted := pkgchat.CompactMessagesLocally(messages, pkgchat.CompactionMaxRecentMessages, debugWriter)
	messagesSaved := len(messages) - len(localCompacted)

	// Only use local compaction if it actually saves enough messages
	if messagesSaved < pkgchat.CompactionMinSavings {
		if c.config.UI.Debug {
			fmt.Fprintf(os.Stderr, "[DEBUG] Local compaction skipped - only saved %d messages (threshold: %d)\n",
				messagesSaved, pkgchat.CompactionMinSavings)
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
