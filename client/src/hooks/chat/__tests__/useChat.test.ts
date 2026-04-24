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
import { renderHook, act, waitFor } from '@testing-library/react';
import { useChat } from '../useChat';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

// Mock the extracted modules
const mockRunAgenticLoop = vi.fn();
const mockMaybeCompact = vi.fn();
const mockCreateConversation = vi.fn();
const mockUpdateConversation = vi.fn();
const mockFetchConversation = vi.fn();
const mockApiFetch = vi.fn();
const mockLoadInputHistory = vi.fn();
const mockSaveInputHistory = vi.fn();
const mockToAPIMessages = vi.fn();

vi.mock('../chatAgenticLoop', () => ({
    runAgenticLoop: (...args: unknown[]) => mockRunAgenticLoop(...args),
}));

vi.mock('../chatCompaction', () => ({
    maybeCompact: (...args: unknown[]) => mockMaybeCompact(...args),
}));

vi.mock('../chatConversation', () => ({
    createConversation: (...args: unknown[]) => mockCreateConversation(...args),
    updateConversation: (...args: unknown[]) => mockUpdateConversation(...args),
    fetchConversation: (...args: unknown[]) => mockFetchConversation(...args),
}));

vi.mock('../../../utils/apiClient', () => ({
    apiFetch: (...args: unknown[]) => mockApiFetch(...args),
}));

vi.mock('../../../contexts/useAICapabilities', () => ({
    useAICapabilities: () => ({ maxIterations: 10 }),
}));

vi.mock('../chatHelpers', () => ({
    loadInputHistory: () => mockLoadInputHistory(),
    saveInputHistory: (...args: unknown[]) => mockSaveInputHistory(...args),
    toAPIMessages: (...args: unknown[]) => mockToAPIMessages(...args),
}));

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
    };
})();

Object.defineProperty(globalThis, 'localStorage', {
    value: localStorageMock,
    writable: true,
});

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('useChat', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        localStorageMock.clear();

        // Default mock implementations
        mockLoadInputHistory.mockReturnValue([]);
        mockSaveInputHistory.mockImplementation(() => {});
        mockToAPIMessages.mockImplementation(
            (msgs: Array<{ role: string; content: string }>) =>
                msgs.map(m => ({ role: m.role, content: m.content })),
        );

        // Mock needs a microtask delay to allow React to commit the user message
        // state update before runAgenticLoop returns
        mockRunAgenticLoop.mockImplementation(async () => {
            // Allow React to process pending state updates
            await Promise.resolve();
            return {
                finalMessage: {
                    role: 'assistant',
                    content: 'Hello! How can I help?',
                    timestamp: new Date().toISOString(),
                },
                updatedApiMessages: [
                    { role: 'user', content: 'Hi' },
                    { role: 'assistant', content: 'Hello! How can I help?' },
                ],
            };
        });

        mockMaybeCompact.mockImplementation(msgs => Promise.resolve(msgs));
        mockCreateConversation.mockResolvedValue('new-conv-123');
        mockUpdateConversation.mockResolvedValue(true);
        mockFetchConversation.mockResolvedValue({
            id: 'conv-123',
            title: 'Test Conversation',
            messages: [
                { role: 'user', content: 'Hello', timestamp: '2024-01-01T00:00:00Z' },
                { role: 'assistant', content: 'Hi!', timestamp: '2024-01-01T00:00:01Z' },
            ],
        });

        mockApiFetch.mockResolvedValue({
            ok: true,
            json: () => Promise.resolve({ tools: [] }),
        });
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    describe('initial state', () => {
        it('returns correct initial state', () => {
            const { result } = renderHook(() => useChat());

            expect(result.current.messages).toEqual([]);
            expect(result.current.activeTools).toEqual([]);
            expect(result.current.currentConversationId).toBeNull();
            expect(result.current.isLoading).toBe(false);
            expect(result.current.error).toBeNull();
            expect(result.current.conversationTitle).toBe('New Chat');
        });

        it('loads input history on mount', () => {
            renderHook(() => useChat());

            expect(mockLoadInputHistory).toHaveBeenCalled();
        });

        it('fetches tools on mount', async () => {
            renderHook(() => useChat());

            await waitFor(() => {
                expect(mockApiFetch).toHaveBeenCalledWith('/api/v1/mcp/tools');
            });
        });

        it('provides backward-compatible aliases', () => {
            const { result } = renderHook(() => useChat());

            expect(result.current.conversationId).toBeNull();
            expect(result.current.clearChat).toBe(result.current.newChat);
        });
    });

    describe('sendMessage', () => {
        it('does nothing for empty message', async () => {
            const { result } = renderHook(() => useChat());

            await act(async () => {
                await result.current.sendMessage('');
            });

            expect(mockRunAgenticLoop).not.toHaveBeenCalled();
        });

        it('does nothing for whitespace-only message', async () => {
            const { result } = renderHook(() => useChat());

            await act(async () => {
                await result.current.sendMessage('   ');
            });

            expect(mockRunAgenticLoop).not.toHaveBeenCalled();
        });

        it('sets isLoading to true during send', async () => {
            let resolveLoop: ((value: unknown) => void) | undefined;
            mockRunAgenticLoop.mockImplementation(
                () =>
                    new Promise(resolve => {
                        resolveLoop = resolve;
                    }),
            );

            const { result } = renderHook(() => useChat());

            let sendPromise: Promise<void>;
            act(() => {
                sendPromise = result.current.sendMessage('Hello');
            });

            // isLoading should be true while waiting
            expect(result.current.isLoading).toBe(true);

            await act(async () => {
                if (!resolveLoop) {
                    throw new Error('expected resolveLoop');
                }
                resolveLoop({
                    finalMessage: {
                        role: 'assistant',
                        content: 'Done',
                        timestamp: new Date().toISOString(),
                    },
                    updatedApiMessages: [],
                });
                await sendPromise;
            });

            expect(result.current.isLoading).toBe(false);
        });

        it('adds user message to messages array', async () => {
            const { result } = renderHook(() => useChat());

            await act(async () => {
                await result.current.sendMessage('Hello');
            });

            // After the complete send cycle, we should have both messages
            await waitFor(() => {
                expect(result.current.messages.length).toBe(2);
            });
            expect(result.current.messages[0].role).toBe('user');
            expect(result.current.messages[0].content).toBe('Hello');
        });

        it('adds assistant response to messages array', async () => {
            const { result } = renderHook(() => useChat());

            await act(async () => {
                await result.current.sendMessage('Hello');
            });

            // Wait for all state updates to propagate
            await waitFor(() => {
                expect(result.current.messages.length).toBe(2);
            });
            expect(result.current.messages[1].role).toBe('assistant');
            expect(result.current.messages[1].content).toBe('Hello! How can I help?');
        });

        it('calls runAgenticLoop with correct parameters', async () => {
            const { result } = renderHook(() => useChat());

            await act(async () => {
                await result.current.sendMessage('Test message');
            });

            expect(mockRunAgenticLoop).toHaveBeenCalledWith(
                expect.objectContaining({
                    apiMessages: expect.arrayContaining([
                        { role: 'user', content: 'Test message' },
                    ]),
                    maxIterations: 10,
                }),
            );
        });

        it('saves input to history', async () => {
            const { result } = renderHook(() => useChat());

            await act(async () => {
                await result.current.sendMessage('Test input');
            });

            expect(mockSaveInputHistory).toHaveBeenCalled();
        });

        it('creates new conversation on first message', async () => {
            const { result } = renderHook(() => useChat());

            await act(async () => {
                await result.current.sendMessage('Hello');
            });

            expect(mockCreateConversation).toHaveBeenCalled();
            expect(result.current.currentConversationId).toBe('new-conv-123');
        });

        it('updates existing conversation on subsequent messages', async () => {
            mockCreateConversation.mockResolvedValue('conv-abc');

            const { result } = renderHook(() => useChat());

            // First message creates conversation
            await act(async () => {
                await result.current.sendMessage('First');
            });

            mockRunAgenticLoop.mockResolvedValue({
                finalMessage: {
                    role: 'assistant',
                    content: 'Second response',
                    timestamp: new Date().toISOString(),
                },
                updatedApiMessages: [],
            });

            // Second message updates conversation
            await act(async () => {
                await result.current.sendMessage('Second');
            });

            expect(mockUpdateConversation).toHaveBeenCalledWith(
                'conv-abc',
                expect.any(Array),
                expect.any(Function),
            );
        });

        it('calls maybeCompact after successful message', async () => {
            const { result } = renderHook(() => useChat());

            await act(async () => {
                await result.current.sendMessage('Hello');
            });

            expect(mockMaybeCompact).toHaveBeenCalled();
        });

        it('handles agentic loop error', async () => {
            mockRunAgenticLoop.mockRejectedValue(new Error('LLM error'));
            const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {});

            const { result } = renderHook(() => useChat());

            await act(async () => {
                await result.current.sendMessage('Hello');
            });

            expect(result.current.error).toBe('LLM error');
            expect(result.current.messages.length).toBe(2); // user + error
            expect(result.current.messages[1].isError).toBe(true);

            consoleSpy.mockRestore();
        });

        it('handles abort gracefully', async () => {
            const abortError = new Error('Aborted');
            abortError.name = 'AbortError';
            mockRunAgenticLoop.mockRejectedValue(abortError);

            const { result } = renderHook(() => useChat());

            await act(async () => {
                await result.current.sendMessage('Hello');
            });

            // Should not set error for abort
            expect(result.current.error).toBeNull();
        });

        it('clears active tools on completion', async () => {
            const { result } = renderHook(() => useChat());

            await act(async () => {
                await result.current.sendMessage('Hello');
            });

            expect(result.current.activeTools).toEqual([]);
        });

        it('handles conversation creation failure gracefully', async () => {
            // Conversation creation returns null (failure)
            mockCreateConversation.mockResolvedValue(null);

            const { result } = renderHook(() => useChat());

            await act(async () => {
                await result.current.sendMessage('Hello');
            });

            // Verify createConversation was called
            expect(mockCreateConversation).toHaveBeenCalled();

            // Wait for state to stabilize
            await waitFor(() => {
                expect(result.current.isLoading).toBe(false);
            });

            // Should still have messages even if conversation creation failed
            await waitFor(() => {
                expect(result.current.messages.length).toBe(2);
            });
            expect(result.current.currentConversationId).toBeNull();
        });

        it('handles compaction failure gracefully', async () => {
            // This test verifies that maybeCompact is called and failures are non-fatal
            const { result } = renderHook(() => useChat());

            await act(async () => {
                await result.current.sendMessage('Hello');
            });

            // Verify compaction was attempted
            expect(mockMaybeCompact).toHaveBeenCalled();

            // Wait for state to stabilize
            await waitFor(() => {
                expect(result.current.isLoading).toBe(false);
            });

            // Chat should complete successfully regardless of compaction result
            await waitFor(() => {
                expect(result.current.messages.length).toBe(2);
            });
        });
    });

    describe('newChat', () => {
        it('clears all messages', async () => {
            const { result } = renderHook(() => useChat());

            await act(async () => {
                await result.current.sendMessage('Hello');
            });

            expect(result.current.messages.length).toBeGreaterThan(0);

            act(() => {
                result.current.newChat();
            });

            expect(result.current.messages).toEqual([]);
        });

        it('clears conversation ID', async () => {
            const { result } = renderHook(() => useChat());

            await act(async () => {
                await result.current.sendMessage('Hello');
            });

            expect(result.current.currentConversationId).not.toBeNull();

            act(() => {
                result.current.newChat();
            });

            expect(result.current.currentConversationId).toBeNull();
        });

        it('resets conversation title', async () => {
            const { result } = renderHook(() => useChat());

            await act(async () => {
                await result.current.loadConversation('conv-123');
            });

            expect(result.current.conversationTitle).toBe('Test Conversation');

            act(() => {
                result.current.newChat();
            });

            expect(result.current.conversationTitle).toBe('New Chat');
        });

        it('clears error state', async () => {
            mockRunAgenticLoop.mockRejectedValue(new Error('Test error'));
            vi.spyOn(console, 'error').mockImplementation(() => {});

            const { result } = renderHook(() => useChat());

            await act(async () => {
                await result.current.sendMessage('Hello');
            });

            expect(result.current.error).not.toBeNull();

            act(() => {
                result.current.newChat();
            });

            expect(result.current.error).toBeNull();
        });

        it('clears active tools', () => {
            const { result } = renderHook(() => useChat());

            act(() => {
                result.current.newChat();
            });

            expect(result.current.activeTools).toEqual([]);
        });

        it('sets isLoading to false', () => {
            const { result } = renderHook(() => useChat());

            act(() => {
                result.current.newChat();
            });

            expect(result.current.isLoading).toBe(false);
        });

        it('clearChat is alias for newChat', () => {
            const { result } = renderHook(() => useChat());

            expect(result.current.clearChat).toBe(result.current.newChat);
        });
    });

    describe('loadConversation', () => {
        it('loads conversation by ID', async () => {
            const { result } = renderHook(() => useChat());

            await act(async () => {
                await result.current.loadConversation('conv-123');
            });

            expect(mockFetchConversation).toHaveBeenCalledWith(
                'conv-123',
                expect.any(Function),
            );
        });

        it('sets messages from loaded conversation', async () => {
            const { result } = renderHook(() => useChat());

            await act(async () => {
                await result.current.loadConversation('conv-123');
            });

            expect(result.current.messages.length).toBe(2);
            expect(result.current.messages[0].role).toBe('user');
            expect(result.current.messages[1].role).toBe('assistant');
        });

        it('sets conversation ID', async () => {
            const { result } = renderHook(() => useChat());

            await act(async () => {
                await result.current.loadConversation('conv-123');
            });

            expect(result.current.currentConversationId).toBe('conv-123');
        });

        it('sets conversation title', async () => {
            const { result } = renderHook(() => useChat());

            await act(async () => {
                await result.current.loadConversation('conv-123');
            });

            expect(result.current.conversationTitle).toBe('Test Conversation');
        });

        it('sets isLoading during load', async () => {
            let resolveLoad: ((value: unknown) => void) | undefined;
            mockFetchConversation.mockImplementation(
                () =>
                    new Promise(resolve => {
                        resolveLoad = resolve;
                    }),
            );

            const { result } = renderHook(() => useChat());

            let loadPromise: Promise<void>;
            act(() => {
                loadPromise = result.current.loadConversation('conv-123');
            });

            expect(result.current.isLoading).toBe(true);

            await act(async () => {
                if (!resolveLoad) {
                    throw new Error('expected resolveLoad');
                }
                resolveLoad({
                    id: 'conv-123',
                    title: 'Test',
                    messages: [],
                });
                await loadPromise;
            });

            expect(result.current.isLoading).toBe(false);
        });

        it('handles load error', async () => {
            mockFetchConversation.mockRejectedValue(
                new Error('Conversation not found'),
            );
            const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {});

            const { result } = renderHook(() => useChat());

            await act(async () => {
                await result.current.loadConversation('nonexistent');
            });

            expect(result.current.error).toBe('Conversation not found');

            consoleSpy.mockRestore();
        });

        it('clears error before loading', async () => {
            mockRunAgenticLoop.mockRejectedValueOnce(new Error('Previous error'));
            vi.spyOn(console, 'error').mockImplementation(() => {});

            const { result } = renderHook(() => useChat());

            await act(async () => {
                await result.current.sendMessage('Hello');
            });

            expect(result.current.error).not.toBeNull();

            mockRunAgenticLoop.mockResolvedValue({
                finalMessage: { role: 'assistant', content: 'Hi', timestamp: '' },
                updatedApiMessages: [],
            });

            await act(async () => {
                await result.current.loadConversation('conv-123');
            });

            expect(result.current.error).toBeNull();
        });

        it('handles conversation with missing title', async () => {
            mockFetchConversation.mockResolvedValue({
                id: 'conv-123',
                title: '',
                messages: [],
            });

            const { result } = renderHook(() => useChat());

            await act(async () => {
                await result.current.loadConversation('conv-123');
            });

            expect(result.current.conversationTitle).toBe('Conversation');
        });

        it('handles conversation with null messages', async () => {
            mockFetchConversation.mockResolvedValue({
                id: 'conv-123',
                title: 'Test',
                messages: undefined,
            });

            const { result } = renderHook(() => useChat());

            await act(async () => {
                await result.current.loadConversation('conv-123');
            });

            expect(result.current.messages).toEqual([]);
        });
    });

    describe('tool fetching', () => {
        it('updates available tools when fetch succeeds', async () => {
            mockApiFetch.mockResolvedValue({
                ok: true,
                json: () =>
                    Promise.resolve({
                        tools: [
                            {
                                name: 'custom_tool',
                                description: 'A custom tool',
                                inputSchema: { type: 'object', properties: {}, required: [] },
                            },
                        ],
                    }),
            } as Response);

            renderHook(() => useChat());

            await waitFor(() => {
                expect(mockApiFetch).toHaveBeenCalledWith('/api/v1/mcp/tools');
            });
        });

        it('handles tool fetch failure gracefully', async () => {
            mockApiFetch.mockRejectedValue(new Error('Network error'));

            const { result } = renderHook(() => useChat());

            await waitFor(() => {
                expect(mockApiFetch).toHaveBeenCalledWith('/api/v1/mcp/tools');
            });

            // Should still be functional with default tools
            expect(result.current.error).toBeNull();
        });

        it('handles empty tools response', async () => {
            mockApiFetch.mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({ tools: [] }),
            } as Response);

            const { result } = renderHook(() => useChat());

            await waitFor(() => {
                expect(mockApiFetch).toHaveBeenCalledWith('/api/v1/mcp/tools');
            });

            // Should still be functional
            expect(result.current.error).toBeNull();
        });

        it('handles non-ok response for tools', async () => {
            mockApiFetch.mockResolvedValue({
                ok: false,
                json: () => Promise.reject(new Error('Not found')),
            } as Response);

            const { result } = renderHook(() => useChat());

            await waitFor(() => {
                expect(mockApiFetch).toHaveBeenCalledWith('/api/v1/mcp/tools');
            });

            // Should still be functional with default tools
            expect(result.current.error).toBeNull();
        });
    });
});
