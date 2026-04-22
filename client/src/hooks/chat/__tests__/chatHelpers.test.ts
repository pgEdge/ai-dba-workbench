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
import { APIMessage } from '../chatTypes';
import { ChatMessageData } from '../../../components/ChatPanel/ChatMessage';

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
            // "Hello world" = 11 chars, 11/4 = 2.75, ceil = 3
            const result = estimateTokenCount(msgs);
            expect(result).toBe(3);
        });

        it('estimates tokens for multiple messages', () => {
            const msgs: APIMessage[] = [
                { role: 'user', content: 'Hello' },          // 5 chars
                { role: 'assistant', content: 'Hi there!' }, // 9 chars
            ];
            // Total 14 chars, 14/4 = 3.5, ceil = 4
            const result = estimateTokenCount(msgs);
            expect(result).toBe(4);
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
            // JSON.stringify of the content array
            const serialized = JSON.stringify(msgs[0].content);
            const expectedTokens = Math.ceil(serialized.length / 4);

            const result = estimateTokenCount(msgs);
            expect(result).toBe(expectedTokens);
        });

        it('handles mixed string and array content', () => {
            const msgs: APIMessage[] = [
                { role: 'user', content: 'Test' },  // 4 chars
                {
                    role: 'assistant',
                    content: [{ type: 'text', text: 'Response' }],
                },
            ];
            const arrayContent = JSON.stringify(msgs[1].content);
            const totalChars = 4 + arrayContent.length;
            const expectedTokens = Math.ceil(totalChars / 4);

            const result = estimateTokenCount(msgs);
            expect(result).toBe(expectedTokens);
        });

        it('handles empty string content', () => {
            const msgs: APIMessage[] = [
                { role: 'user', content: '' },
            ];
            const result = estimateTokenCount(msgs);
            expect(result).toBe(0);
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
            const serialized = JSON.stringify(msgs[0].content);
            const expectedTokens = Math.ceil(serialized.length / 4);

            const result = estimateTokenCount(msgs);
            expect(result).toBe(expectedTokens);
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
            const parsedSaved = JSON.parse(savedValue as string);

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
            const parsedSaved = JSON.parse(savedValue as string);

            expect(parsedSaved).toEqual(history);
        });

        it('writes exactly INPUT_HISTORY_MAX items when history equals max', () => {
            const exactHistory = Array.from(
                { length: INPUT_HISTORY_MAX },
                (_, i) => `query${i}`
            );

            saveInputHistory(exactHistory);

            const savedValue = localStorageMock.setItem.mock.calls[0][1];
            const parsedSaved = JSON.parse(savedValue as string);

            expect(parsedSaved).toHaveLength(INPUT_HISTORY_MAX);
        });
    });
});
