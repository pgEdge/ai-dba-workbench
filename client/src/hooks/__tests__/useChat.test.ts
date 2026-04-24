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

const mockApiFetch = vi.fn();

vi.mock('../../utils/apiClient', () => ({
    apiFetch: (...args: unknown[]) => mockApiFetch(...args),
}));

const mockMaxIterations = 10;

vi.mock('../../contexts/useAICapabilities', () => ({
    useAICapabilities: () => ({ maxIterations: mockMaxIterations }),
}));

// Mock localStorage
const localStorageMock = (() => {
    let store: Record<string, string> = {};
    return {
        getItem: vi.fn((key: string) => store[key] || null),
        setItem: vi.fn((key: string, value: string) => {
            store[key] = value;
        }),
        clear: vi.fn(() => {
            store = {};
        }),
    };
})();

Object.defineProperty(window, 'localStorage', {
    value: localStorageMock,
});

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeTextResponse(text = 'Hello!') {
    return {
        ok: true,
        text: vi.fn().mockResolvedValue(''),
        json: vi.fn().mockResolvedValue({
            content: [{ type: 'text', text }],
            stop_reason: 'end_turn',
        }),
    };
}

function makeToolUseResponse(toolName = 'list_connections', toolId = 'tool_1') {
    return {
        ok: true,
        text: vi.fn().mockResolvedValue(''),
        json: vi.fn().mockResolvedValue({
            content: [
                { type: 'tool_use', id: toolId, name: toolName, input: {} },
            ],
            stop_reason: 'tool_use',
        }),
    };
}

function makeToolCallResponse(text = 'Tool result', isError = false) {
    return {
        ok: true,
        text: vi.fn().mockResolvedValue(''),
        json: vi.fn().mockResolvedValue({
            content: [{ text }],
            isError,
        }),
    };
}

function makeErrorResponse(errorText = 'Error occurred') {
    return {
        ok: false,
        text: vi.fn().mockResolvedValue(errorText),
        json: vi.fn(),
    };
}

function makeToolsResponse() {
    return {
        ok: true,
        text: vi.fn().mockResolvedValue(''),
        json: vi.fn().mockResolvedValue({
            tools: [
                {
                    name: 'list_connections',
                    description: 'List connections',
                    inputSchema: { type: 'object', properties: {}, required: [] },
                },
            ],
        }),
    };
}

function makeConversationCreateResponse(id = 'conv-1') {
    return {
        ok: true,
        text: vi.fn().mockResolvedValue(''),
        json: vi.fn().mockResolvedValue({
            id,
            title: 'New Conversation',
        }),
    };
}

function makeConversationLoadResponse() {
    return {
        ok: true,
        text: vi.fn().mockResolvedValue(''),
        json: vi.fn().mockResolvedValue({
            id: 'conv-1',
            title: 'Loaded Conversation',
            messages: [
                { role: 'user', content: 'Previous message', timestamp: '2024-01-01T00:00:00Z' },
                { role: 'assistant', content: 'Previous response', timestamp: '2024-01-01T00:01:00Z' },
            ],
        }),
    };
}


// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('useChat', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        localStorageMock.clear();

        // Default mock for tools fetch
        mockApiFetch.mockImplementation((url: string) => {
            if (url === '/api/v1/mcp/tools') {
                return Promise.resolve(makeToolsResponse());
            }
            return Promise.resolve(makeTextResponse());
        });
    });

    afterEach(() => {
        vi.resetAllMocks();
    });

    it('returns initial state', async () => {
        const { result } = renderHook(() => useChat());

        await waitFor(() => {
            expect(result.current.messages).toEqual([]);
        });

        expect(result.current.activeTools).toEqual([]);
        expect(result.current.currentConversationId).toBeNull();
        expect(result.current.conversationId).toBeNull();
        expect(result.current.conversationTitle).toBe('New Chat');
        expect(result.current.isLoading).toBe(false);
        expect(result.current.error).toBeNull();
        expect(typeof result.current.sendMessage).toBe('function');
        expect(typeof result.current.newChat).toBe('function');
        expect(typeof result.current.loadConversation).toBe('function');
        expect(typeof result.current.clearChat).toBe('function');
    });

    it('fetches available tools on mount', async () => {
        renderHook(() => useChat());

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledWith('/api/v1/mcp/tools');
        });
    });

    it('sendMessage does nothing for empty text', async () => {
        const { result } = renderHook(() => useChat());

        await act(async () => {
            await result.current.sendMessage('   ');
        });

        expect(result.current.messages).toEqual([]);
        // Only the tools fetch should have been called
        expect(mockApiFetch).toHaveBeenCalledTimes(1);
    });

    it('sendMessage adds user message and gets response', async () => {
        mockApiFetch.mockImplementation((url: string, options?: RequestInit) => {
            if (url === '/api/v1/mcp/tools') {
                return Promise.resolve(makeToolsResponse());
            }
            if (url === '/api/v1/llm/chat') {
                return Promise.resolve(makeTextResponse('Hello user!'));
            }
            if (url === '/api/v1/conversations' && options?.method === 'POST') {
                return Promise.resolve(makeConversationCreateResponse());
            }
            if (url.startsWith('/api/v1/conversations/') && options?.method === 'PUT') {
                return Promise.resolve({ ok: true, text: vi.fn(), json: vi.fn() });
            }
            return Promise.resolve(makeTextResponse());
        });

        const { result } = renderHook(() => useChat());

        // Wait for tools to load
        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledWith('/api/v1/mcp/tools');
        });

        await act(async () => {
            await result.current.sendMessage('Hello');
        });

        // Verify at least one message and assistant response is present
        expect(result.current.messages.length).toBeGreaterThanOrEqual(1);

        // Find the assistant message
        const assistantMessage = result.current.messages.find(m => m.role === 'assistant');
        expect(assistantMessage).toBeDefined();
        expect(assistantMessage?.content).toBe('Hello user!');

        // Verify LLM was called
        const llmCalls = mockApiFetch.mock.calls.filter(
            call => call[0] === '/api/v1/llm/chat'
        );
        expect(llmCalls.length).toBe(1);
    });

    it('sendMessage sets isLoading during API call', async () => {
        let resolveChat: ((value: unknown) => void) | undefined;

        mockApiFetch.mockImplementation((url: string) => {
            if (url === '/api/v1/mcp/tools') {
                return Promise.resolve(makeToolsResponse());
            }
            if (url === '/api/v1/llm/chat') {
                return new Promise(resolve => {
                    resolveChat = resolve;
                });
            }
            return Promise.resolve(makeTextResponse());
        });

        const { result } = renderHook(() => useChat());

        let sendPromise: Promise<void>;
        act(() => {
            sendPromise = result.current.sendMessage('Test');
        });

        await waitFor(() => {
            expect(result.current.isLoading).toBe(true);
        });

        await act(async () => {
            resolveChat!(makeTextResponse());
            await sendPromise;
        });

        expect(result.current.isLoading).toBe(false);
    });

    it('sendMessage handles tool use response', async () => {
        let callCount = 0;

        mockApiFetch.mockImplementation((url: string, options?: RequestInit) => {
            if (url === '/api/v1/mcp/tools') {
                return Promise.resolve(makeToolsResponse());
            }
            if (url === '/api/v1/llm/chat') {
                callCount++;
                if (callCount === 1) {
                    return Promise.resolve(makeToolUseResponse('list_connections', 'tool_1'));
                }
                return Promise.resolve(makeTextResponse('Here are your connections.'));
            }
            if (url === '/api/v1/mcp/tools/call') {
                return Promise.resolve(makeToolCallResponse('[{"id": 1, "name": "db1"}]'));
            }
            if (url === '/api/v1/conversations' && options?.method === 'POST') {
                return Promise.resolve(makeConversationCreateResponse());
            }
            if (url.startsWith('/api/v1/conversations/') && options?.method === 'PUT') {
                return Promise.resolve({ ok: true, text: vi.fn(), json: vi.fn() });
            }
            return Promise.resolve(makeTextResponse());
        });

        const { result } = renderHook(() => useChat());

        // Wait for tools to load
        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledWith('/api/v1/mcp/tools');
        });

        await act(async () => {
            await result.current.sendMessage('List my connections');
        });

        // LLM should be called twice: first returns tool_use, second returns text
        const llmCalls = mockApiFetch.mock.calls.filter(
            call => call[0] === '/api/v1/llm/chat'
        );
        expect(llmCalls.length).toBe(2);

        // Tool should be called once
        const toolCalls = mockApiFetch.mock.calls.filter(
            call => call[0] === '/api/v1/mcp/tools/call'
        );
        expect(toolCalls.length).toBe(1);

        // Verify we have messages (at least assistant message should be there)
        expect(result.current.messages.length).toBeGreaterThanOrEqual(1);
    });

    it('sendMessage sets error on API failure', async () => {
        mockApiFetch.mockImplementation((url: string) => {
            if (url === '/api/v1/mcp/tools') {
                return Promise.resolve(makeToolsResponse());
            }
            if (url === '/api/v1/llm/chat') {
                return Promise.resolve(makeErrorResponse('Service unavailable'));
            }
            return Promise.resolve(makeTextResponse());
        });

        const { result } = renderHook(() => useChat());

        await act(async () => {
            await result.current.sendMessage('Test');
        });

        await waitFor(() => {
            expect(result.current.error).toContain('Service unavailable');
        });

        // Should add error message to conversation
        expect(result.current.messages).toHaveLength(2);
        expect(result.current.messages[1].isError).toBe(true);
    });

    it('sendMessage creates conversation on first response', async () => {
        mockApiFetch.mockImplementation((url: string) => {
            if (url === '/api/v1/mcp/tools') {
                return Promise.resolve(makeToolsResponse());
            }
            if (url === '/api/v1/llm/chat') {
                return Promise.resolve(makeTextResponse('Response'));
            }
            if (url === '/api/v1/conversations') {
                return Promise.resolve(makeConversationCreateResponse('new-conv-123'));
            }
            return Promise.resolve(makeTextResponse());
        });

        const { result } = renderHook(() => useChat());

        await act(async () => {
            await result.current.sendMessage('First message');
        });

        await waitFor(() => {
            expect(result.current.currentConversationId).toBe('new-conv-123');
        });

        expect(mockApiFetch).toHaveBeenCalledWith(
            '/api/v1/conversations',
            expect.objectContaining({ method: 'POST' }),
        );
    });

    it('sendMessage updates existing conversation on subsequent messages', async () => {
        mockApiFetch.mockImplementation((url: string, options?: { method?: string }) => {
            if (url === '/api/v1/mcp/tools') {
                return Promise.resolve(makeToolsResponse());
            }
            if (url === '/api/v1/llm/chat') {
                return Promise.resolve(makeTextResponse('Response'));
            }
            if (url === '/api/v1/conversations' && options?.method === 'POST') {
                return Promise.resolve(makeConversationCreateResponse('conv-xyz'));
            }
            if (url.startsWith('/api/v1/conversations/') && options?.method === 'PUT') {
                return Promise.resolve({ ok: true, text: vi.fn(), json: vi.fn() });
            }
            return Promise.resolve(makeTextResponse());
        });

        const { result } = renderHook(() => useChat());

        // First message - creates conversation
        await act(async () => {
            await result.current.sendMessage('First');
        });

        await waitFor(() => {
            expect(result.current.currentConversationId).toBe('conv-xyz');
        });

        // Second message - should update existing conversation
        await act(async () => {
            await result.current.sendMessage('Second');
        });

        const updateCalls = mockApiFetch.mock.calls.filter(
            call => call[0] === '/api/v1/conversations/conv-xyz' && call[1]?.method === 'PUT'
        );
        expect(updateCalls.length).toBe(1);
    });

    it('sendMessage saves input to history', async () => {
        mockApiFetch.mockImplementation((url: string) => {
            if (url === '/api/v1/mcp/tools') {
                return Promise.resolve(makeToolsResponse());
            }
            if (url === '/api/v1/llm/chat') {
                return Promise.resolve(makeTextResponse());
            }
            if (url === '/api/v1/conversations') {
                return Promise.resolve(makeConversationCreateResponse());
            }
            return Promise.resolve(makeTextResponse());
        });

        const { result } = renderHook(() => useChat());

        await act(async () => {
            await result.current.sendMessage('First query');
        });

        await act(async () => {
            await result.current.sendMessage('Second query');
        });

        expect(result.current.inputHistory).toContain('First query');
        expect(result.current.inputHistory).toContain('Second query');
        expect(localStorageMock.setItem).toHaveBeenCalled();
    });

    it('newChat clears all state', async () => {
        mockApiFetch.mockImplementation((url: string, options?: RequestInit) => {
            if (url === '/api/v1/mcp/tools') {
                return Promise.resolve(makeToolsResponse());
            }
            if (url === '/api/v1/llm/chat') {
                return Promise.resolve(makeTextResponse('Response'));
            }
            if (url === '/api/v1/conversations' && options?.method === 'POST') {
                return Promise.resolve(makeConversationCreateResponse('conv-1'));
            }
            if (url.startsWith('/api/v1/conversations/') && options?.method === 'PUT') {
                return Promise.resolve({ ok: true, text: vi.fn(), json: vi.fn() });
            }
            return Promise.resolve(makeTextResponse());
        });

        const { result } = renderHook(() => useChat());

        // Wait for tools to load
        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledWith('/api/v1/mcp/tools');
        });

        // Send a message first
        await act(async () => {
            await result.current.sendMessage('Hello');
        });

        // Verify at least one message was added
        expect(result.current.messages.length).toBeGreaterThanOrEqual(1);

        // Clear chat
        act(() => {
            result.current.newChat();
        });

        expect(result.current.messages).toEqual([]);
        expect(result.current.currentConversationId).toBeNull();
        expect(result.current.conversationTitle).toBe('New Chat');
        expect(result.current.error).toBeNull();
        expect(result.current.isLoading).toBe(false);
    });

    it('clearChat is an alias for newChat', () => {
        const { result } = renderHook(() => useChat());

        expect(result.current.clearChat).toBe(result.current.newChat);
    });

    it('loadConversation fetches and sets conversation data', async () => {
        mockApiFetch.mockImplementation((url: string) => {
            if (url === '/api/v1/mcp/tools') {
                return Promise.resolve(makeToolsResponse());
            }
            if (url === '/api/v1/conversations/conv-load') {
                return Promise.resolve(makeConversationLoadResponse());
            }
            return Promise.resolve(makeTextResponse());
        });

        const { result } = renderHook(() => useChat());

        await act(async () => {
            await result.current.loadConversation('conv-load');
        });

        await waitFor(() => {
            expect(result.current.messages).toHaveLength(2);
        });

        expect(result.current.currentConversationId).toBe('conv-load');
        expect(result.current.conversationTitle).toBe('Loaded Conversation');
        expect(result.current.messages[0].content).toBe('Previous message');
    });

    it('loadConversation sets error on failure', async () => {
        mockApiFetch.mockImplementation((url: string) => {
            if (url === '/api/v1/mcp/tools') {
                return Promise.resolve(makeToolsResponse());
            }
            if (url.startsWith('/api/v1/conversations/')) {
                return Promise.resolve(makeErrorResponse('Conversation not found'));
            }
            return Promise.resolve(makeTextResponse());
        });

        const { result } = renderHook(() => useChat());

        await act(async () => {
            await result.current.loadConversation('invalid-id');
        });

        await waitFor(() => {
            expect(result.current.error).toContain('Conversation not found');
        });
    });

    it('loads input history from localStorage on mount', () => {
        const savedHistory = ['previous query 1', 'previous query 2'];
        localStorageMock.getItem.mockReturnValue(JSON.stringify(savedHistory));

        const { result } = renderHook(() => useChat());

        expect(result.current.inputHistory).toEqual(savedHistory);
    });

    it('handles corrupted localStorage gracefully', () => {
        localStorageMock.getItem.mockReturnValue('not valid json');

        const { result } = renderHook(() => useChat());

        expect(result.current.inputHistory).toEqual([]);
    });

    it('handles tool execution error', async () => {
        let callCount = 0;

        mockApiFetch.mockImplementation((url: string, options?: RequestInit) => {
            if (url === '/api/v1/mcp/tools') {
                return Promise.resolve(makeToolsResponse());
            }
            if (url === '/api/v1/llm/chat') {
                callCount++;
                if (callCount === 1) {
                    return Promise.resolve(makeToolUseResponse());
                }
                return Promise.resolve(makeTextResponse('Done'));
            }
            if (url === '/api/v1/mcp/tools/call') {
                return Promise.resolve(makeToolCallResponse('Tool error', true));
            }
            if (url === '/api/v1/conversations' && options?.method === 'POST') {
                return Promise.resolve(makeConversationCreateResponse());
            }
            if (url.startsWith('/api/v1/conversations/') && options?.method === 'PUT') {
                return Promise.resolve({ ok: true, text: vi.fn(), json: vi.fn() });
            }
            return Promise.resolve(makeTextResponse());
        });

        const { result } = renderHook(() => useChat());

        // Wait for tools to load
        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledWith('/api/v1/mcp/tools');
        });

        await act(async () => {
            await result.current.sendMessage('Run tool');
        });

        // Verify at least one message (assistant response)
        expect(result.current.messages.length).toBeGreaterThanOrEqual(1);

        // Should have called LLM twice (first for tool_use, second for final response)
        const llmCalls = mockApiFetch.mock.calls.filter(
            call => call[0] === '/api/v1/llm/chat'
        );
        expect(llmCalls.length).toBe(2);
    });

    it('limits iterations to maxIterations', async () => {
        // Always return tool_use to force max iterations
        let toolCallCount = 0;

        mockApiFetch.mockImplementation((url: string, options?: RequestInit) => {
            if (url === '/api/v1/mcp/tools') {
                return Promise.resolve(makeToolsResponse());
            }
            if (url === '/api/v1/llm/chat') {
                return Promise.resolve(makeToolUseResponse('list_connections', `tool_${toolCallCount++}`));
            }
            if (url === '/api/v1/mcp/tools/call') {
                return Promise.resolve(makeToolCallResponse('Result'));
            }
            if (url === '/api/v1/conversations' && options?.method === 'POST') {
                return Promise.resolve(makeConversationCreateResponse());
            }
            if (url.startsWith('/api/v1/conversations/') && options?.method === 'PUT') {
                return Promise.resolve({ ok: true, text: vi.fn(), json: vi.fn() });
            }
            return Promise.resolve(makeTextResponse());
        });

        const { result } = renderHook(() => useChat());

        // Wait for tools to load
        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledWith('/api/v1/mcp/tools');
        });

        await act(async () => {
            await result.current.sendMessage('Loop forever');
        });

        // Should have at least 1 message (the error message about exceeding iterations)
        expect(result.current.messages.length).toBeGreaterThanOrEqual(1);

        // The last message should be an error about exceeding iterations
        const lastMessage = result.current.messages[result.current.messages.length - 1];
        expect(lastMessage.content).toContain('unable to complete');
        expect(lastMessage.isError).toBe(true);

        // LLM should have been called maxIterations times
        const llmCalls = mockApiFetch.mock.calls.filter(
            call => call[0] === '/api/v1/llm/chat'
        );
        expect(llmCalls.length).toBe(mockMaxIterations);
    });

    it('deduplicates input history entries', async () => {
        mockApiFetch.mockImplementation((url: string) => {
            if (url === '/api/v1/mcp/tools') {
                return Promise.resolve(makeToolsResponse());
            }
            if (url === '/api/v1/llm/chat') {
                return Promise.resolve(makeTextResponse());
            }
            if (url === '/api/v1/conversations') {
                return Promise.resolve(makeConversationCreateResponse());
            }
            return Promise.resolve(makeTextResponse());
        });

        const { result } = renderHook(() => useChat());

        await act(async () => {
            await result.current.sendMessage('duplicate');
        });

        await act(async () => {
            await result.current.sendMessage('different');
        });

        await act(async () => {
            await result.current.sendMessage('duplicate');
        });

        // 'duplicate' should only appear once (most recent)
        const duplicateCount = result.current.inputHistory.filter(
            h => h === 'duplicate'
        ).length;
        expect(duplicateCount).toBe(1);
        expect(result.current.inputHistory[0]).toBe('duplicate'); // Most recent first
    });
});
