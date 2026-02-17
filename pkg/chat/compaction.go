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
	"fmt"
	"io"
)

// Compaction thresholds used by both CLI and server chat clients.
const (
	// CompactionMaxRecentMessages is the number of recent messages to
	// preserve during compaction.
	CompactionMaxRecentMessages = 10

	// CompactionMaxTokens is the maximum token budget passed to
	// server-side compaction requests.
	CompactionMaxTokens = 100000

	// CompactionTokenThreshold triggers compaction when estimated
	// tokens exceed this value.
	CompactionTokenThreshold = 15000

	// CompactionMinMessages is the minimum message count before
	// message-count-based compaction is considered.
	CompactionMinMessages = 15

	// CompactionMinSavings requires at least this many messages to be
	// removed for compaction to be worthwhile.
	CompactionMinSavings = 5
)

// ShouldCompact determines whether compaction should be attempted for
// the given message slice. It returns two booleans: whether to compact
// by token count and whether to compact by message count.
func ShouldCompact(messages []Message) (byTokens bool, byMessages bool) {
	estimatedTokens := EstimateTotalTokens(messages)
	byTokens = estimatedTokens > CompactionTokenThreshold
	byMessages = len(messages) >= CompactionMinMessages
	return byTokens, byMessages
}

// CompactMessagesLocally performs basic local compaction on a message
// slice. It keeps the first user message (to preserve original context)
// and the most recent maxRecentMessages, ensuring that tool_use /
// tool_result pairs are not split.
//
// The debug parameter controls whether diagnostic output is written to
// debugWriter. Pass nil to suppress debug output.
func CompactMessagesLocally(messages []Message, maxRecentMessages int, debugWriter io.Writer) []Message {
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
	startIdx = AdjustStartForToolPairs(messages, startIdx)

	compacted = append(compacted, messages[startIdx:]...)

	if debugWriter != nil {
		fmt.Fprintf(debugWriter, "[DEBUG] Local compaction: %d -> %d (kept first + last %d)\n",
			len(messages), len(compacted), maxRecentMessages)
	}

	return compacted
}

// AdjustStartForToolPairs adjusts the start index to ensure
// tool_use/tool_result message pairs are kept together. If the message
// at startIdx contains tool_results, the preceding assistant message
// (which contains the tool_use blocks) is included.
func AdjustStartForToolPairs(messages []Message, startIdx int) int {
	if startIdx <= 1 || startIdx >= len(messages) {
		return startIdx
	}

	// Check if the message at startIdx is a user message with tool_results
	msg := messages[startIdx]
	if msg.Role != "user" {
		return startIdx
	}

	// Check if this message contains tool_result blocks
	if HasToolResults(msg) {
		// Include the preceding assistant message (which should have tool_use)
		if startIdx > 1 {
			startIdx--
		}
	}

	return startIdx
}
