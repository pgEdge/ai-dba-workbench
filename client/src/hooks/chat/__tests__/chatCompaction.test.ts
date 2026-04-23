/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { maybeCompact, FetchFunction } from '../chatCompaction';
import { APIMessage } from '../chatTypes';
import {
    COMPACTION_MAX_TOKENS,
    COMPACTION_RECENT_WINDOW,
} from '../chatConstants';

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

/**
 * Create a mock fetch function that returns the specified response.
 */
function createMockFetch(
    ok: boolean,
    data?: object,
    text?: string,
): FetchFunction {
    return vi.fn().mockResolvedValue({
        ok,
        json: vi.fn().mockResolvedValue(data ?? {}),
        text: vi.fn().mockResolvedValue(text ?? ''),
    });
}

/**
 * Create a large message array that exceeds the compaction threshold.
 * Each message contributes approximately 4 + (content.length / 4) tokens.
 */
function createLargeMessageArray(): APIMessage[] {
    // To exceed COMPACTION_TOKEN_THRESHOLD (80,000), we need messages
    // that sum to at least 80,000 tokens.
    // Each message with 400 chars of content = 4 + 100 = 104 tokens.
    // 80,000 / 104 = ~769 messages needed.
    const messages: APIMessage[] = [];
    const content = 'x'.repeat(400); // 400 chars = 100 tokens + 4 overhead
    for (let i = 0; i < 800; i++) {
        messages.push({
            role: i % 2 === 0 ? 'user' : 'assistant',
            content,
        });
    }
    return messages;
}

/**
 * Create a small message array that is below the compaction threshold.
 */
function createSmallMessageArray(): APIMessage[] {
    return [
        { role: 'user', content: 'Hello' },
        { role: 'assistant', content: 'Hi there!' },
    ];
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('chatCompaction', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    describe('maybeCompact', () => {
        it('returns original messages when below token threshold', async () => {
            const msgs = createSmallMessageArray();
            const mockFetch = createMockFetch(true);

            const result = await maybeCompact(msgs, mockFetch);

            expect(result).toBe(msgs);
            expect(mockFetch).not.toHaveBeenCalled();
        });

        it('returns original messages for empty array', async () => {
            const msgs: APIMessage[] = [];
            const mockFetch = createMockFetch(true);

            const result = await maybeCompact(msgs, mockFetch);

            expect(result).toBe(msgs);
            expect(mockFetch).not.toHaveBeenCalled();
        });

        it('calls compaction endpoint when above threshold', async () => {
            const msgs = createLargeMessageArray();
            const compactedMsgs = [
                { role: 'assistant', content: 'Summary of conversation' },
            ];
            const mockFetch = createMockFetch(true, {
                messages: compactedMsgs,
            });

            const result = await maybeCompact(msgs, mockFetch);

            expect(mockFetch).toHaveBeenCalledWith(
                '/api/v1/chat/compact',
                expect.objectContaining({
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                }),
            );
            expect(result).toEqual(compactedMsgs);
        });

        it('sends correct request body to compaction endpoint', async () => {
            const msgs = createLargeMessageArray();
            const mockFetch = createMockFetch(true, { messages: msgs });

            await maybeCompact(msgs, mockFetch);

            const call = mockFetch.mock.calls[0];
            const body = JSON.parse(call[1]?.body as string);

            expect(body).toEqual({
                messages: msgs,
                max_tokens: COMPACTION_MAX_TOKENS,
                recent_window: COMPACTION_RECENT_WINDOW,
                keep_anchors: true,
                options: {
                    preserve_tool_results: true,
                    enable_summarization: true,
                },
            });
        });

        it('returns original messages when compaction endpoint fails', async () => {
            const msgs = createLargeMessageArray();
            const mockFetch = createMockFetch(false, undefined, 'Server error');

            const result = await maybeCompact(msgs, mockFetch);

            expect(result).toBe(msgs);
        });

        it('returns original messages when compaction response has no messages', async () => {
            const msgs = createLargeMessageArray();
            const mockFetch = createMockFetch(true, { messages: null });

            const result = await maybeCompact(msgs, mockFetch);

            expect(result).toBe(msgs);
        });

        it('returns original messages when compaction response is empty object', async () => {
            const msgs = createLargeMessageArray();
            const mockFetch = createMockFetch(true, {});

            const result = await maybeCompact(msgs, mockFetch);

            expect(result).toBe(msgs);
        });

        it('returns original messages when fetch throws an error', async () => {
            const msgs = createLargeMessageArray();
            const mockFetch = vi.fn().mockRejectedValue(new Error('Network error'));
            const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {});

            const result = await maybeCompact(msgs, mockFetch);

            expect(result).toBe(msgs);
            expect(consoleSpy).toHaveBeenCalledWith(
                'Chat compaction failed:',
                expect.any(Error),
            );
        });

        it('returns original messages when json parsing fails', async () => {
            const msgs = createLargeMessageArray();
            const mockFetch = vi.fn().mockResolvedValue({
                ok: true,
                json: vi.fn().mockRejectedValue(new Error('Invalid JSON')),
            });
            const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {});

            const result = await maybeCompact(msgs, mockFetch);

            expect(result).toBe(msgs);
            expect(consoleSpy).toHaveBeenCalled();
        });

        it('handles messages exactly at threshold boundary', async () => {
            // Create messages that are just below the threshold
            // COMPACTION_TOKEN_THRESHOLD is 80,000
            // Each message with 396 chars = 4 + 99 = 103 tokens
            // 776 messages * 103 = 79,928 tokens (just below threshold)
            const messages: APIMessage[] = [];
            const content = 'x'.repeat(396);
            for (let i = 0; i < 776; i++) {
                messages.push({
                    role: i % 2 === 0 ? 'user' : 'assistant',
                    content,
                });
            }
            const mockFetch = createMockFetch(true);

            const result = await maybeCompact(messages, mockFetch);

            // Should not call compaction because we're below threshold
            expect(result).toBe(messages);
            expect(mockFetch).not.toHaveBeenCalled();
        });

        it('compacts when exactly at threshold', async () => {
            // Create messages that are exactly at the threshold
            // Each message with 400 chars = 4 + 100 = 104 tokens
            // 770 messages * 104 = 80,080 tokens (just above threshold)
            const messages: APIMessage[] = [];
            const content = 'x'.repeat(400);
            for (let i = 0; i < 770; i++) {
                messages.push({
                    role: i % 2 === 0 ? 'user' : 'assistant',
                    content,
                });
            }
            const compactedMsgs = [{ role: 'assistant', content: 'Summary' }];
            const mockFetch = createMockFetch(true, { messages: compactedMsgs });

            const result = await maybeCompact(messages, mockFetch);

            expect(mockFetch).toHaveBeenCalled();
            expect(result).toEqual(compactedMsgs);
        });

        it('handles messages with content block arrays', async () => {
            // Create messages with content blocks to exceed threshold
            const messages: APIMessage[] = [];
            const textBlock = { type: 'text', text: 'x'.repeat(400) };
            for (let i = 0; i < 800; i++) {
                messages.push({
                    role: 'assistant',
                    content: [textBlock],
                });
            }
            const compactedMsgs = [{ role: 'assistant', content: 'Summary' }];
            const mockFetch = createMockFetch(true, { messages: compactedMsgs });

            const result = await maybeCompact(messages, mockFetch);

            expect(mockFetch).toHaveBeenCalled();
            expect(result).toEqual(compactedMsgs);
        });

        it('returns compacted messages array when valid', async () => {
            const msgs = createLargeMessageArray();
            const compactedMsgs: APIMessage[] = [
                { role: 'user', content: 'Summarized user message' },
                { role: 'assistant', content: 'Summarized assistant response' },
            ];
            const mockFetch = createMockFetch(true, { messages: compactedMsgs });

            const result = await maybeCompact(msgs, mockFetch);

            expect(result).toEqual(compactedMsgs);
            expect(result).not.toBe(msgs);
        });

        it('does not modify original messages array', async () => {
            const msgs = createLargeMessageArray();
            const originalLength = msgs.length;
            const compactedMsgs = [{ role: 'assistant', content: 'Summary' }];
            const mockFetch = createMockFetch(true, { messages: compactedMsgs });

            await maybeCompact(msgs, mockFetch);

            expect(msgs.length).toBe(originalLength);
        });
    });
});
