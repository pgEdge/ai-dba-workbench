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
import {
    estimateTokenCount,
    toAPIMessages,
    loadInputHistory,
    saveInputHistory,
} from '../chatHelpers';
import { INPUT_HISTORY_KEY, INPUT_HISTORY_MAX } from '../chatConstants';
import type { APIMessage } from '../chatTypes';
import type { ChatMessageData } from '../../../components/ChatPanel/ChatMessage';

// ---------------------------------------------------------------------------
// localStorage mock
// ---------------------------------------------------------------------------

const localStorageMock = (() => {
    let store: Record<string, string> = {};
    return {
        getItem: vi.fn((key: string) => store[key] || null),
        setItem: vi.fn((key: string, value: string) => {
            store[key] = value;
        }),
        removeItem: vi.fn((key: string) => {
            delete store[key];
        }),
        clear: vi.fn(() => {
            store = {};
        }),
        _getStore: () => store,
    };
})();

Object.defineProperty(globalThis, 'localStorage', {
    value: localStorageMock,
    writable: true,
});

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('chatHelpers', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        localStorageMock.clear();
    });

    afterEach(() => {
        vi.resetAllMocks();
    });

    describe('estimateTokenCount', () => {
        it('returns 0 for an empty array', () => {
            const result = estimateTokenCount([]);
            expect(result).toBe(0);
        });

        it('estimates tokens for a simple string message', () => {
            const msgs: APIMessage[] = [
                { role: 'user', content: 'Hello world' },
            ];
            // Per-message overhead: 4 tokens
            // "Hello world" = 11 chars, ceil(11/4) = 3 tokens
            // Total: 4 + 3 = 7
            const result = estimateTokenCount(msgs);
            expect(result).toBe(7);
        });

        it('estimates tokens for multiple messages', () => {
            const msgs: APIMessage[] = [
                { role: 'user', content: 'Hello' },          // 4 + ceil(5/4) = 4 + 2 = 6
                { role: 'assistant', content: 'Hi there!' }, // 4 + ceil(9/4) = 4 + 3 = 7
            ];
            // Total: 6 + 7 = 13
            const result = estimateTokenCount(msgs);
            expect(result).toBe(13);
        });

        it('estimates tokens for content block arrays', () => {
            const msgs: APIMessage[] = [
                {
                    role: 'assistant',
                    content: [
                        { type: 'text', text: 'Hello' },
                        { type: 'tool_use', id: 'tool1', name: 'test', input: {} },
                    ],
                },
            ];
            // Per-message overhead: 4 tokens
            // "Hello" = 5 chars, ceil(5/4) = 2 tokens
            // tool_use block has no 'text' field, so not counted
            // Total: 4 + 2 = 6
            const result = estimateTokenCount(msgs);
            expect(result).toBe(6);
        });

        it('handles mixed string and array content', () => {
            const msgs: APIMessage[] = [
                { role: 'user', content: 'Test' },  // 4 + ceil(4/4) = 4 + 1 = 5
                {
                    role: 'assistant',
                    content: [{ type: 'text', text: 'Response' }], // 4 + ceil(8/4) = 4 + 2 = 6
                },
            ];
            // Total: 5 + 6 = 11
            const result = estimateTokenCount(msgs);
            expect(result).toBe(11);
        });

        it('handles empty string content', () => {
            const msgs: APIMessage[] = [
                { role: 'user', content: '' },
            ];
            // Per-message overhead: 4 tokens
            // Empty string: ceil(0/4) = 0 tokens
            // Total: 4
            const result = estimateTokenCount(msgs);
            expect(result).toBe(4);
        });

        it('handles tool result content', () => {
            const msgs: APIMessage[] = [
                {
                    role: 'user',
                    content: [
                        {
                            type: 'tool_result',
                            tool_use_id: 'tool123',
                            content: 'Result data',
                        },
                    ],
                },
            ];
            // Per-message overhead: 4 tokens
            // tool_result block has 'content' not 'text', so not counted
            // Total: 4
            const result = estimateTokenCount(msgs);
            expect(result).toBe(4);
        });
    });

    describe('toAPIMessages', () => {
        it('returns an empty array for empty input', () => {
            const result = toAPIMessages([]);
            expect(result).toEqual([]);
        });

        it('converts ChatMessageData to APIMessage format', () => {
            const chatMessages: ChatMessageData[] = [
                {
                    role: 'user',
                    content: 'Hello',
                    timestamp: '2024-01-01T00:00:00Z',
                },
                {
                    role: 'assistant',
                    content: 'Hi there!',
                    timestamp: '2024-01-01T00:00:01Z',
                },
            ];

            const result = toAPIMessages(chatMessages);

            expect(result).toHaveLength(2);
            expect(result[0]).toEqual({ role: 'user', content: 'Hello' });
            expect(result[1]).toEqual({ role: 'assistant', content: 'Hi there!' });
        });

        it('filters out system messages', () => {
            const chatMessages: ChatMessageData[] = [
                {
                    role: 'system',
                    content: 'System prompt',
                    timestamp: '2024-01-01T00:00:00Z',
                },
                {
                    role: 'user',
                    content: 'User message',
                    timestamp: '2024-01-01T00:00:01Z',
                },
                {
                    role: 'assistant',
                    content: 'Response',
                    timestamp: '2024-01-01T00:00:02Z',
                },
            ];

            const result = toAPIMessages(chatMessages);

            expect(result).toHaveLength(2);
            expect(result.every(m => m.role !== 'system')).toBe(true);
        });

        it('strips timestamp field from output', () => {
            const chatMessages: ChatMessageData[] = [
                {
                    role: 'user',
                    content: 'Test',
                    timestamp: '2024-01-01T00:00:00Z',
                },
            ];

            const result = toAPIMessages(chatMessages);

            expect(result[0]).not.toHaveProperty('timestamp');
        });

        it('strips isError field from output', () => {
            const chatMessages: ChatMessageData[] = [
                {
                    role: 'assistant',
                    content: 'Error message',
                    timestamp: '2024-01-01T00:00:00Z',
                    isError: true,
                },
            ];

            const result = toAPIMessages(chatMessages);

            expect(result[0]).not.toHaveProperty('isError');
        });

        it('strips activity field from output', () => {
            const chatMessages: ChatMessageData[] = [
                {
                    role: 'assistant',
                    content: 'Processing...',
                    timestamp: '2024-01-01T00:00:00Z',
                    activity: 'Analyzing data',
                },
            ];

            const result = toAPIMessages(chatMessages);

            expect(result[0]).not.toHaveProperty('activity');
        });

        it('preserves content block arrays', () => {
            const contentBlocks = [
                { type: 'text', text: 'Hello' },
                { type: 'tool_use', id: 'tool1', name: 'test', input: {} },
            ];
            const chatMessages: ChatMessageData[] = [
                {
                    role: 'assistant',
                    content: contentBlocks,
                    timestamp: '2024-01-01T00:00:00Z',
                },
            ];

            const result = toAPIMessages(chatMessages);

            expect(result[0].content).toEqual(contentBlocks);
        });

        it('handles only system messages (returns empty)', () => {
            const chatMessages: ChatMessageData[] = [
                {
                    role: 'system',
                    content: 'System only',
                    timestamp: '2024-01-01T00:00:00Z',
                },
            ];

            const result = toAPIMessages(chatMessages);

            expect(result).toEqual([]);
        });
    });

    describe('loadInputHistory', () => {
        it('returns an empty array when localStorage is empty', () => {
            localStorageMock.getItem.mockReturnValue(null);

            const result = loadInputHistory();

            expect(result).toEqual([]);
            expect(localStorageMock.getItem).toHaveBeenCalledWith(INPUT_HISTORY_KEY);
        });

        it('returns parsed array from localStorage', () => {
            const savedHistory = ['query1', 'query2', 'query3'];
            localStorageMock.getItem.mockReturnValue(JSON.stringify(savedHistory));

            const result = loadInputHistory();

            expect(result).toEqual(savedHistory);
        });

        it('handles corrupt localStorage data gracefully', () => {
            localStorageMock.getItem.mockReturnValue('not valid json {{{{');

            const result = loadInputHistory();

            expect(result).toEqual([]);
        });

        it('handles non-array JSON gracefully', () => {
            localStorageMock.getItem.mockReturnValue(
                JSON.stringify({ not: 'an array' })
            );

            const result = loadInputHistory();

            expect(result).toEqual([]);
        });

        it('handles null JSON value gracefully', () => {
            localStorageMock.getItem.mockReturnValue('null');

            const result = loadInputHistory();

            expect(result).toEqual([]);
        });

        it('truncates history to INPUT_HISTORY_MAX', () => {
            const largeHistory = Array.from(
                { length: INPUT_HISTORY_MAX + 20 },
                (_, i) => `query${i}`
            );
            localStorageMock.getItem.mockReturnValue(JSON.stringify(largeHistory));

            const result = loadInputHistory();

            expect(result).toHaveLength(INPUT_HISTORY_MAX);
            expect(result[0]).toBe('query0');
            expect(result[INPUT_HISTORY_MAX - 1]).toBe(`query${INPUT_HISTORY_MAX - 1}`);
        });

        it('returns exact history when length equals INPUT_HISTORY_MAX', () => {
            const exactHistory = Array.from(
                { length: INPUT_HISTORY_MAX },
                (_, i) => `query${i}`
            );
            localStorageMock.getItem.mockReturnValue(JSON.stringify(exactHistory));

            const result = loadInputHistory();

            expect(result).toEqual(exactHistory);
        });

        it('handles empty string in localStorage', () => {
            localStorageMock.getItem.mockReturnValue('');

            const result = loadInputHistory();

            expect(result).toEqual([]);
        });
    });

    describe('saveInputHistory', () => {
        it('writes history to localStorage', () => {
            const history = ['query1', 'query2'];

            saveInputHistory(history);

            expect(localStorageMock.setItem).toHaveBeenCalledWith(
                INPUT_HISTORY_KEY,
                JSON.stringify(history)
            );
        });

        it('handles empty history', () => {
            saveInputHistory([]);

            expect(localStorageMock.setItem).toHaveBeenCalledWith(
                INPUT_HISTORY_KEY,
                '[]'
            );
        });

        it('truncates history to INPUT_HISTORY_MAX', () => {
            const largeHistory = Array.from(
                { length: INPUT_HISTORY_MAX + 30 },
                (_, i) => `query${i}`
            );

            saveInputHistory(largeHistory);

            const savedValue = localStorageMock.setItem.mock.calls[0][1];
            const parsedSaved = JSON.parse(savedValue);

            expect(parsedSaved).toHaveLength(INPUT_HISTORY_MAX);
            expect(parsedSaved[0]).toBe('query0');
            expect(parsedSaved[INPUT_HISTORY_MAX - 1]).toBe(
                `query${INPUT_HISTORY_MAX - 1}`
            );
        });

        it('handles localStorage quota exceeded error gracefully', () => {
            localStorageMock.setItem.mockImplementation(() => {
                throw new Error('QuotaExceededError');
            });

            // Should not throw
            expect(() => saveInputHistory(['query'])).not.toThrow();
        });

        it('handles localStorage access denied error gracefully', () => {
            localStorageMock.setItem.mockImplementation(() => {
                throw new DOMException('Access denied', 'SecurityError');
            });

            // Should not throw
            expect(() => saveInputHistory(['query'])).not.toThrow();
        });

        it('preserves order when saving', () => {
            const history = ['first', 'second', 'third'];

            saveInputHistory(history);

            const savedValue = localStorageMock.setItem.mock.calls[0][1];
            const parsedSaved = JSON.parse(savedValue);

            expect(parsedSaved).toEqual(history);
        });

        it('writes exactly INPUT_HISTORY_MAX items when history equals max', () => {
            const exactHistory = Array.from(
                { length: INPUT_HISTORY_MAX },
                (_, i) => `query${i}`
            );

            saveInputHistory(exactHistory);

            const savedValue = localStorageMock.setItem.mock.calls[0][1];
            const parsedSaved = JSON.parse(savedValue);

            expect(parsedSaved).toHaveLength(INPUT_HISTORY_MAX);
        });
    });
});
