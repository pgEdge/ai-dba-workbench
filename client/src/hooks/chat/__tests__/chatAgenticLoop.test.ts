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
    runAgenticLoop,
    type AgenticLoopParams,
    type FetchFunction,
    ITERATION_LIMIT_MESSAGE,
} from '../chatAgenticLoop';
import type { APIMessage, ToolDefinition } from '../chatTypes';
import type { LLMResponse, ToolCallResponse } from '../../../types/llm';

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

/**
 * Create sample tools for testing.
 */
function createSampleTools(): ToolDefinition[] {
    return [
        {
            name: 'list_connections',
            description: 'List database connections',
            inputSchema: {
                type: 'object',
                properties: {},
                required: [],
            },
        },
        {
            name: 'query_database',
            description: 'Execute SQL query',
            inputSchema: {
                type: 'object',
                properties: {
                    query: { type: 'string', description: 'SQL query' },
                },
                required: ['query'],
            },
        },
    ];
}

/**
 * Create default loop params for testing.
 */
function createLoopParams(
    overrides: Partial<AgenticLoopParams> = {},
): AgenticLoopParams {
    return {
        apiMessages: [{ role: 'user', content: 'Hello' }],
        availableTools: createSampleTools(),
        systemPrompt: 'You are a helpful assistant.',
        maxIterations: 10,
        abortSignal: new AbortController().signal,
        fetchFn: vi.fn(),
        onToolActivity: vi.fn(),
        ...overrides,
    };
}

/**
 * Create an LLM response with only text content (no tool calls).
 */
function createTextResponse(text: string): LLMResponse {
    return {
        content: [{ type: 'text', text }],
        stop_reason: 'end_turn',
    };
}

/**
 * Create an LLM response with tool use requests.
 */
function createToolUseResponse(
    tools: Array<{ id: string; name: string; input: Record<string, unknown> }>,
    text?: string,
): LLMResponse {
    const content = tools.map(t => ({
        type: 'tool_use',
        id: t.id,
        name: t.name,
        input: t.input,
    }));
    if (text) {
        content.unshift({ type: 'text', text, id: '', name: '', input: {} });
    }
    return { content };
}

/**
 * Create a tool call response.
 */
function createToolCallResponse(
    text: string,
    isError = false,
): ToolCallResponse {
    return {
        content: [{ text }],
        isError,
    };
}

/**
 * Create a mock fetch function that returns different responses
 * based on the URL.
 */
function createMockFetch(
    llmResponses: LLMResponse[],
    toolResponses = new Map<string, ToolCallResponse>(),
): FetchFunction {
    let llmCallIndex = 0;

    return vi.fn().mockImplementation(async (url: string, init?: RequestInit) => {
        if (url === '/api/v1/llm/chat') {
            const response = llmResponses[llmCallIndex] ?? createTextResponse('');
            llmCallIndex++;
            return {
                ok: true,
                json: () => Promise.resolve(response),
                text: () => Promise.resolve(''),
            };
        }

        if (url === '/api/v1/mcp/tools/call') {
            const body = JSON.parse(init?.body as string);
            const toolResponse =
                toolResponses.get(body.name) ??
                createToolCallResponse('Tool result');
            return {
                ok: true,
                json: () => Promise.resolve(toolResponse),
                text: () => Promise.resolve(''),
            };
        }

        return {
            ok: false,
            json: () => Promise.reject(new Error('Unknown endpoint')),
            text: () => Promise.resolve('Unknown endpoint'),
        };
    });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('chatAgenticLoop', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        vi.useFakeTimers();
        vi.setSystemTime(new Date('2024-01-15T12:00:00Z'));
    });

    afterEach(() => {
        vi.restoreAllMocks();
        vi.useRealTimers();
    });

    describe('runAgenticLoop', () => {
        describe('simple text responses (no tool calls)', () => {
            it('returns final message when LLM responds with text only', async () => {
                const mockFetch = createMockFetch([
                    createTextResponse('Hello! How can I help?'),
                ]);
                const params = createLoopParams({ fetchFn: mockFetch });

                const result = await runAgenticLoop(params);

                expect(result.finalMessage.role).toBe('assistant');
                expect(result.finalMessage.content).toBe('Hello! How can I help?');
                expect(result.finalMessage.timestamp).toBe('2024-01-15T12:00:00.000Z');
                expect(result.finalMessage.activity).toBeUndefined();
            });

            it('updates API messages with assistant response', async () => {
                const initialMessages: APIMessage[] = [
                    { role: 'user', content: 'Hi' },
                ];
                const mockFetch = createMockFetch([
                    createTextResponse('Hello!'),
                ]);
                const params = createLoopParams({
                    apiMessages: initialMessages,
                    fetchFn: mockFetch,
                });

                const result = await runAgenticLoop(params);

                expect(result.updatedApiMessages).toHaveLength(2);
                expect(result.updatedApiMessages[0]).toEqual({
                    role: 'user',
                    content: 'Hi',
                });
                expect(result.updatedApiMessages[1]).toEqual({
                    role: 'assistant',
                    content: 'Hello!',
                });
            });

            it('joins multiple text blocks with newlines', async () => {
                const mockFetch = createMockFetch([
                    {
                        content: [
                            { type: 'text', text: 'First line' },
                            { type: 'text', text: 'Second line' },
                        ],
                    },
                ]);
                const params = createLoopParams({ fetchFn: mockFetch });

                const result = await runAgenticLoop(params);

                expect(result.finalMessage.content).toBe('First line\nSecond line');
            });

            it('handles empty text response', async () => {
                const mockFetch = createMockFetch([{ content: [] }]);
                const params = createLoopParams({ fetchFn: mockFetch });

                const result = await runAgenticLoop(params);

                expect(result.finalMessage.content).toBe('');
            });
        });

        describe('tool execution', () => {
            it('executes single tool and returns final response', async () => {
                const toolResponses = new Map([
                    ['list_connections', createToolCallResponse('Connection 1, Connection 2')],
                ]);
                const mockFetch = createMockFetch(
                    [
                        createToolUseResponse([
                            { id: 'tool-1', name: 'list_connections', input: {} },
                        ]),
                        createTextResponse('Here are your connections: Connection 1, Connection 2'),
                    ],
                    toolResponses,
                );
                const onToolActivity = vi.fn();
                const params = createLoopParams({
                    fetchFn: mockFetch,
                    onToolActivity,
                });

                const result = await runAgenticLoop(params);

                expect(result.finalMessage.content).toContain('Here are your connections');
                expect(result.finalMessage.activity).toHaveLength(1);
                const activity = result.finalMessage.activity;
                if (!activity) {
                    throw new Error('expected activity');
                }
                expect(activity[0].name).toBe('list_connections');
                expect(activity[0].status).toBe('completed');
            });

            it('executes multiple tools sequentially', async () => {
                const toolResponses = new Map([
                    ['list_connections', createToolCallResponse('Conn 1')],
                    ['query_database', createToolCallResponse('Query result')],
                ]);
                const mockFetch = createMockFetch(
                    [
                        createToolUseResponse([
                            { id: 'tool-1', name: 'list_connections', input: {} },
                            { id: 'tool-2', name: 'query_database', input: { query: 'SELECT 1' } },
                        ]),
                        createTextResponse('Done'),
                    ],
                    toolResponses,
                );
                const onToolActivity = vi.fn();
                const params = createLoopParams({
                    fetchFn: mockFetch,
                    onToolActivity,
                });

                const result = await runAgenticLoop(params);

                expect(result.finalMessage.activity).toHaveLength(2);
                expect(onToolActivity).toHaveBeenCalled();
            });

            it('handles tool error and continues', async () => {
                const toolResponses = new Map([
                    ['list_connections', createToolCallResponse('Error: permission denied', true)],
                ]);
                const mockFetch = createMockFetch(
                    [
                        createToolUseResponse([
                            { id: 'tool-1', name: 'list_connections', input: {} },
                        ]),
                        createTextResponse('Sorry, I could not list connections.'),
                    ],
                    toolResponses,
                );
                const onToolActivity = vi.fn();
                const params = createLoopParams({
                    fetchFn: mockFetch,
                    onToolActivity,
                });

                const result = await runAgenticLoop(params);

                const activity = result.finalMessage.activity;
                if (!activity) {
                    throw new Error('expected activity');
                }
                expect(activity[0].status).toBe('error');
            });

            it('handles tool call network failure', async () => {
                let llmCallCount = 0;
                const mockFetch = vi.fn().mockImplementation(async (url: string) => {
                    if (url === '/api/v1/llm/chat') {
                        llmCallCount++;
                        if (llmCallCount === 1) {
                            return {
                                ok: true,
                                json: () =>
                                    Promise.resolve(
                                        createToolUseResponse([
                                            { id: 'tool-1', name: 'list_connections', input: {} },
                                        ]),
                                    ),
                            };
                        }
                        return {
                            ok: true,
                            json: () => Promise.resolve(createTextResponse('Error handled')),
                        };
                    }
                    if (url === '/api/v1/mcp/tools/call') {
                        return {
                            ok: false,
                            status: 500,
                            text: () => Promise.resolve('Internal Server Error'),
                        };
                    }
                    return { ok: false, text: () => Promise.resolve('') };
                });

                const onToolActivity = vi.fn();
                const params = createLoopParams({
                    fetchFn: mockFetch,
                    onToolActivity,
                });

                const result = await runAgenticLoop(params);

                const activity = result.finalMessage.activity;
                if (!activity) {
                    throw new Error('expected activity');
                }
                expect(activity[0].status).toBe('error');
            });

            it('appends tool results to API messages', async () => {
                const toolResponses = new Map([
                    ['list_connections', createToolCallResponse('Result data')],
                ]);
                const mockFetch = createMockFetch(
                    [
                        createToolUseResponse([
                            { id: 'tool-abc', name: 'list_connections', input: {} },
                        ]),
                        createTextResponse('Done'),
                    ],
                    toolResponses,
                );
                const params = createLoopParams({
                    apiMessages: [{ role: 'user', content: 'List connections' }],
                    fetchFn: mockFetch,
                });

                const result = await runAgenticLoop(params);

                // Messages: user, assistant (tool_use), user (tool_result), assistant (text)
                expect(result.updatedApiMessages.length).toBeGreaterThanOrEqual(4);

                // Find the tool result message
                const toolResultMsg = result.updatedApiMessages.find(
                    m => m.role === 'user' && Array.isArray(m.content),
                );
                expect(toolResultMsg).toBeDefined();
            });

            it('handles tool with missing id', async () => {
                const mockFetch = createMockFetch(
                    [
                        {
                            content: [
                                {
                                    type: 'tool_use',
                                    name: 'list_connections',
                                    input: {},
                                    // No id field
                                },
                            ],
                        },
                        createTextResponse('Done'),
                    ],
                    new Map([
                        ['list_connections', createToolCallResponse('Result')],
                    ]),
                );
                const params = createLoopParams({ fetchFn: mockFetch });

                const result = await runAgenticLoop(params);

                // Should complete without error, using empty string for id
                expect(result.finalMessage.content).toBe('Done');
            });

            it('handles tool with missing name', async () => {
                const mockFetch = createMockFetch(
                    [
                        {
                            content: [
                                {
                                    type: 'tool_use',
                                    id: 'tool-1',
                                    input: {},
                                    // No name field
                                },
                            ],
                        },
                        createTextResponse('Done'),
                    ],
                    new Map(),
                );
                const onToolActivity = vi.fn();
                const params = createLoopParams({
                    fetchFn: mockFetch,
                    onToolActivity,
                });

                const result = await runAgenticLoop(params);

                // Should use 'unknown' as tool name
                const activity = result.finalMessage.activity;
                if (!activity) {
                    throw new Error('expected activity');
                }
                expect(activity[0].name).toBe('unknown');
            });

            it('handles tool response with no content', async () => {
                const mockFetch = createMockFetch(
                    [
                        createToolUseResponse([
                            { id: 'tool-1', name: 'list_connections', input: {} },
                        ]),
                        createTextResponse('Done'),
                    ],
                    new Map([
                        ['list_connections', { isError: false }],
                    ]),
                );
                const params = createLoopParams({ fetchFn: mockFetch });

                const result = await runAgenticLoop(params);

                expect(result.finalMessage.content).toBe('Done');
            });

            it('handles tool response isError with no content text', async () => {
                const mockFetch = createMockFetch(
                    [
                        createToolUseResponse([
                            { id: 'tool-1', name: 'list_connections', input: {} },
                        ]),
                        createTextResponse('Error handled'),
                    ],
                    new Map([
                        ['list_connections', { content: [], isError: true }],
                    ]),
                );
                const params = createLoopParams({ fetchFn: mockFetch });

                const result = await runAgenticLoop(params);

                const activity = result.finalMessage.activity;
                if (!activity) {
                    throw new Error('expected activity');
                }
                expect(activity[0].status).toBe('error');
            });
        });

        describe('multi-iteration scenarios', () => {
            it('handles multiple iterations with different tools', async () => {
                const toolResponses = new Map([
                    ['list_connections', createToolCallResponse('Connections: A, B')],
                    ['query_database', createToolCallResponse('Query result: 42')],
                ]);
                const mockFetch = createMockFetch(
                    [
                        createToolUseResponse([
                            { id: 't1', name: 'list_connections', input: {} },
                        ]),
                        createToolUseResponse([
                            { id: 't2', name: 'query_database', input: { query: 'SELECT 1' } },
                        ]),
                        createTextResponse('Final answer: 42'),
                    ],
                    toolResponses,
                );
                const params = createLoopParams({ fetchFn: mockFetch });

                const result = await runAgenticLoop(params);

                expect(result.finalMessage.content).toBe('Final answer: 42');
                expect(result.finalMessage.activity).toHaveLength(2);
            });
        });

        describe('iteration limit', () => {
            it('returns error when max iterations exceeded', async () => {
                // Always return tool use, never text
                const mockFetch = createMockFetch(
                    Array(10).fill(
                        createToolUseResponse([
                            { id: 'tool-1', name: 'list_connections', input: {} },
                        ]),
                    ),
                    new Map([['list_connections', createToolCallResponse('Result')]]),
                );
                const params = createLoopParams({
                    maxIterations: 3,
                    fetchFn: mockFetch,
                });

                const result = await runAgenticLoop(params);

                expect(result.finalMessage.content).toBe(ITERATION_LIMIT_MESSAGE);
                expect(result.finalMessage.isError).toBe(true);
            });

            it('includes tool activity in error message when limit exceeded', async () => {
                const mockFetch = createMockFetch(
                    Array(5).fill(
                        createToolUseResponse([
                            { id: 'tool-1', name: 'list_connections', input: {} },
                        ]),
                    ),
                    new Map([['list_connections', createToolCallResponse('Result')]]),
                );
                const params = createLoopParams({
                    maxIterations: 2,
                    fetchFn: mockFetch,
                });

                const result = await runAgenticLoop(params);

                expect(result.finalMessage.activity).toHaveLength(2);
            });

            it('appends error message to API messages when limit exceeded', async () => {
                const mockFetch = createMockFetch(
                    Array(5).fill(
                        createToolUseResponse([
                            { id: 'tool-1', name: 'list_connections', input: {} },
                        ]),
                    ),
                    new Map([['list_connections', createToolCallResponse('Result')]]),
                );
                const params = createLoopParams({
                    maxIterations: 1,
                    fetchFn: mockFetch,
                });

                const result = await runAgenticLoop(params);

                const lastMessage =
                    result.updatedApiMessages[result.updatedApiMessages.length - 1];
                expect(lastMessage.role).toBe('assistant');
                expect(lastMessage.content).toBe(ITERATION_LIMIT_MESSAGE);
            });
        });

        describe('abort handling', () => {
            it('throws AbortError when signal is already aborted', async () => {
                const abortController = new AbortController();
                abortController.abort();
                const params = createLoopParams({
                    abortSignal: abortController.signal,
                });

                await expect(runAgenticLoop(params)).rejects.toThrow('Aborted');
            });

            it('throws AbortError when aborted during LLM call', async () => {
                const abortController = new AbortController();
                const mockFetch = vi.fn().mockImplementation(async () => {
                    abortController.abort();
                    const error = new Error('Aborted');
                    error.name = 'AbortError';
                    throw error;
                });
                const params = createLoopParams({
                    fetchFn: mockFetch,
                    abortSignal: abortController.signal,
                });

                await expect(runAgenticLoop(params)).rejects.toThrow('Aborted');
            });

            it('throws AbortError when aborted during tool call', async () => {
                const abortController = new AbortController();
                const mockFetch = vi.fn().mockImplementation(async (url: string) => {
                    if (url === '/api/v1/llm/chat') {
                        return {
                            ok: true,
                            json: () =>
                                Promise.resolve(
                                    createToolUseResponse([
                                        { id: 't1', name: 'list_connections', input: {} },
                                    ]),
                                ),
                        };
                    }
                    if (url === '/api/v1/mcp/tools/call') {
                        abortController.abort();
                        const error = new Error('Aborted');
                        error.name = 'AbortError';
                        throw error;
                    }
                    return { ok: false, text: () => Promise.resolve('') };
                });
                const params = createLoopParams({
                    fetchFn: mockFetch,
                    abortSignal: abortController.signal,
                });

                await expect(runAgenticLoop(params)).rejects.toThrow('Aborted');
            });
        });

        describe('error handling', () => {
            it('throws error when LLM request fails', async () => {
                const mockFetch = vi.fn().mockResolvedValue({
                    ok: false,
                    text: () => Promise.resolve('Rate limit exceeded'),
                });
                const params = createLoopParams({ fetchFn: mockFetch });

                await expect(runAgenticLoop(params)).rejects.toThrow(
                    'LLM request failed: Rate limit exceeded',
                );
            });

            it('throws error when LLM returns invalid JSON', async () => {
                const mockFetch = vi.fn().mockResolvedValue({
                    ok: true,
                    json: () => Promise.reject(new Error('Invalid JSON')),
                });
                const params = createLoopParams({ fetchFn: mockFetch });

                await expect(runAgenticLoop(params)).rejects.toThrow('Invalid JSON');
            });

            it('handles network error during LLM call', async () => {
                const mockFetch = vi.fn().mockRejectedValue(new Error('Network error'));
                const params = createLoopParams({ fetchFn: mockFetch });

                await expect(runAgenticLoop(params)).rejects.toThrow('Network error');
            });

            it('records tool execution error in results', async () => {
                let callCount = 0;
                const mockFetch = vi.fn().mockImplementation(async (url: string) => {
                    if (url === '/api/v1/llm/chat') {
                        callCount++;
                        if (callCount === 1) {
                            return {
                                ok: true,
                                json: () =>
                                    Promise.resolve(
                                        createToolUseResponse([
                                            { id: 't1', name: 'list_connections', input: {} },
                                        ]),
                                    ),
                            };
                        }
                        return {
                            ok: true,
                            json: () => Promise.resolve(createTextResponse('Done')),
                        };
                    }
                    if (url === '/api/v1/mcp/tools/call') {
                        throw new Error('Connection refused');
                    }
                    return { ok: false, text: () => Promise.resolve('') };
                });
                const params = createLoopParams({ fetchFn: mockFetch });

                const result = await runAgenticLoop(params);

                // Tool error should be recorded and loop should continue
                const activity = result.finalMessage.activity;
                if (!activity) {
                    throw new Error('expected activity');
                }
                expect(activity[0].status).toBe('error');
            });
        });

        describe('API message formatting', () => {
            it('sends correct request body to LLM endpoint', async () => {
                const mockFetch = createMockFetch([createTextResponse('Hi')]);
                const params = createLoopParams({
                    apiMessages: [{ role: 'user', content: 'Hello' }],
                    systemPrompt: 'Be helpful',
                    fetchFn: mockFetch,
                });

                await runAgenticLoop(params);

                expect(mockFetch).toHaveBeenCalledWith(
                    '/api/v1/llm/chat',
                    expect.objectContaining({
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                    }),
                );

                const call = mockFetch.mock.calls[0];
                const body = JSON.parse(call[1]?.body as string);
                expect(body.messages).toEqual([{ role: 'user', content: 'Hello' }]);
                expect(body.system).toBe('Be helpful');
                expect(body.tools).toEqual(params.availableTools);
            });

            it('sends correct request body to tool call endpoint', async () => {
                const mockFetch = createMockFetch(
                    [
                        createToolUseResponse([
                            { id: 't1', name: 'query_database', input: { query: 'SELECT 1' } },
                        ]),
                        createTextResponse('Done'),
                    ],
                    new Map([['query_database', createToolCallResponse('1')]]),
                );
                const params = createLoopParams({ fetchFn: mockFetch });

                await runAgenticLoop(params);

                // Find the tool call
                const toolCall = mockFetch.mock.calls.find(
                    c => c[0] === '/api/v1/mcp/tools/call',
                );
                if (!toolCall) {
                    throw new Error('expected toolCall');
                }

                const body = JSON.parse(toolCall[1]?.body as string);
                expect(body.name).toBe('query_database');
                expect(body.arguments).toEqual({ query: 'SELECT 1' });
            });

            it('passes abort signal to fetch calls', async () => {
                const abortController = new AbortController();
                const mockFetch = createMockFetch([createTextResponse('Hi')]);
                const params = createLoopParams({
                    fetchFn: mockFetch,
                    abortSignal: abortController.signal,
                });

                await runAgenticLoop(params);

                expect(mockFetch).toHaveBeenCalledWith(
                    '/api/v1/llm/chat',
                    expect.objectContaining({
                        signal: abortController.signal,
                    }),
                );
            });
        });

        describe('tool activity callbacks', () => {
            it('calls onToolActivity when tool starts running', async () => {
                const mockFetch = createMockFetch(
                    [
                        createToolUseResponse([
                            { id: 't1', name: 'list_connections', input: {} },
                        ]),
                        createTextResponse('Done'),
                    ],
                    new Map([['list_connections', createToolCallResponse('Result')]]),
                );
                const activityHistory: Array<Array<{ status: string }>> = [];
                const onToolActivity = vi.fn().mockImplementation(activities => {
                    // Capture a copy of each activity state
                    activityHistory.push(activities.map((a: { status: string }) => ({ ...a })));
                });
                const params = createLoopParams({
                    fetchFn: mockFetch,
                    onToolActivity,
                });

                await runAgenticLoop(params);

                // Should be called at least twice: once for running, once for completed
                expect(onToolActivity).toHaveBeenCalled();
                // First call should have status 'running'
                expect(activityHistory[0][0].status).toBe('running');
            });

            it('calls onToolActivity when tool completes', async () => {
                const mockFetch = createMockFetch(
                    [
                        createToolUseResponse([
                            { id: 't1', name: 'list_connections', input: {} },
                        ]),
                        createTextResponse('Done'),
                    ],
                    new Map([['list_connections', createToolCallResponse('Result')]]),
                );
                const onToolActivity = vi.fn();
                const params = createLoopParams({
                    fetchFn: mockFetch,
                    onToolActivity,
                });

                await runAgenticLoop(params);

                const lastCall = onToolActivity.mock.calls[onToolActivity.mock.calls.length - 1][0];
                expect(lastCall[0].status).toBe('completed');
            });

            it('calls onToolActivity when tool errors', async () => {
                const mockFetch = createMockFetch(
                    [
                        createToolUseResponse([
                            { id: 't1', name: 'list_connections', input: {} },
                        ]),
                        createTextResponse('Error handled'),
                    ],
                    new Map([['list_connections', createToolCallResponse('Error', true)]]),
                );
                const onToolActivity = vi.fn();
                const params = createLoopParams({
                    fetchFn: mockFetch,
                    onToolActivity,
                });

                await runAgenticLoop(params);

                const lastCall = onToolActivity.mock.calls[onToolActivity.mock.calls.length - 1][0];
                expect(lastCall[0].status).toBe('error');
            });
        });
    });
});
