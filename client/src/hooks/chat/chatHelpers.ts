/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { ChatMessageData } from '../../components/ChatPanel/ChatMessage';
import { LLMContentBlock, ToolResult } from '../../types/llm';
import { APIMessage } from './chatTypes';
import { INPUT_HISTORY_KEY, INPUT_HISTORY_MAX } from './chatConstants';

// ---------------------------------------------------------------
// Token estimation
// ---------------------------------------------------------------

/**
 * Estimate token count from an array of API messages.
 * Adds per-message overhead (4 tokens for role, formatting, delimiters)
 * plus content length divided by 4.
 */
export function estimateTokenCount(msgs: APIMessage[]): number {
    let totalTokens = 0;
    for (const msg of msgs) {
        // Per-message overhead (role, formatting, delimiters)
        totalTokens += 4;
        if (typeof msg.content === 'string') {
            totalTokens += Math.ceil(msg.content.length / 4);
        } else if (Array.isArray(msg.content)) {
            for (const block of msg.content) {
                if ('text' in block) {
                    totalTokens += Math.ceil(block.text.length / 4);
                }
            }
        }
    }
    return totalTokens;
}

// ---------------------------------------------------------------
// Message conversion
// ---------------------------------------------------------------

/**
 * Convert ChatMessageData[] to the API message format, stripping
 * UI-only fields (timestamp, isError, activity).  System messages
 * are excluded because the system prompt is sent separately.
 */
export function toAPIMessages(chatMessages: ChatMessageData[]): APIMessage[] {
    return chatMessages
        .filter(m => m.role !== 'system')
        .map(m => ({
            role: m.role,
            content: m.content as string | LLMContentBlock[] | ToolResult[],
        }));
}

// ---------------------------------------------------------------
// Input history persistence
// ---------------------------------------------------------------

// NOTE: Chat input history is intentionally retained in localStorage
// across sessions and is not cleared on logout. This preserves the
// user's recent queries for a better experience.

/**
 * Load input history from localStorage.
 */
export function loadInputHistory(): string[] {
    try {
        const stored = localStorage.getItem(INPUT_HISTORY_KEY);
        if (stored) {
            const parsed = JSON.parse(stored);
            if (Array.isArray(parsed)) {
                return parsed.slice(0, INPUT_HISTORY_MAX);
            }
        }
    } catch {
        // Ignore parse errors from corrupt storage
    }
    return [];
}

/**
 * Persist input history to localStorage.
 */
export function saveInputHistory(history: string[]): void {
    try {
        localStorage.setItem(
            INPUT_HISTORY_KEY,
            JSON.stringify(history.slice(0, INPUT_HISTORY_MAX)),
        );
    } catch {
        // Ignore quota or access errors
    }
}
