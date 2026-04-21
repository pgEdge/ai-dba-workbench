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
import { runAgenticLoop, AgenticLoopOptions } from '../agenticLoop';

// Mock dependencies
vi.mock('../apiClient', () => ({
    apiFetch: vi.fn(),
}));

vi.mock('../toolDisplayNames', () => ({
    getToolDisplayName: vi.fn((name: string) => {
        const displayNames: Record<string, string> = {
            query_database: 'Querying database',
            get_schema_info: 'Inspecting schema',
        };
        return displayNames[name] || name;
    }),
}));

vi.mock('../textHelpers', () => ({
    stripPreamble: vi.fn((text: string) => text),
}));

import { apiFetch } from '../apiClient';
import { stripPreamble } from '../textHelpers';

const mockApiFetch = apiFetch as ReturnType<typeof vi.fn>;
const mockStripPreamble = stripPreamble as ReturnType<typeof vi.fn>;

describe('runAgenticLoop', () => {
    const createMockResponse = (
        data: unknown,
        ok = true,
    ): Partial<Response> => ({
        ok,
        json: vi.fn().mockResolvedValue(data),
        text: vi.fn().mockResolvedValue(JSON.stringify(data)),
    });

    const baseOptions: AgenticLoopOptions = {
        messages: [{ role: 'user', content: 'Test query' }],
        tools: [],
        systemPrompt: 'You are a helpful assistant.',
        maxIterations: 5,
    };

    beforeEach(() => {
        vi.clearAllMocks();
        mockStripPreamble.mockImplementation((text: string) => text);
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    describe('simple text responses', () => {
        it('returns final text response when no tools are requested', async () => {
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({
                    content: [{ type: 'text', text: 'Hello, how can I help?' }],
                }),
            );

            const result = await runAgenticLoop(baseOptions);

            expect(result).toBe('Hello, how can I help?');
        });

        it('joins multiple text blocks', async () => {
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({
                    content: [
                        { type: 'text', text: 'First part.' },
                        { type: 'text', text: 'Second part.' },
                    ],
                }),
            );

            const result = await runAgenticLoop(baseOptions);

            expect(result).toBe('First part.\nSecond part.');
        });

        it('returns empty string when no text content', async () => {
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({ content: [] }),
            );

            const result = await runAgenticLoop(baseOptions);

            expect(result).toBe('');
        });

        it('handles undefined content gracefully', async () => {
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({}),
            );

            const result = await runAgenticLoop(baseOptions);

            expect(result).toBe('');
        });

        it('strips preamble from final response', async () => {
            mockStripPreamble.mockReturnValue('## Stripped content');
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({
                    content: [{ type: 'text', text: 'Preamble here\n## Stripped content' }],
                }),
            );

            const result = await runAgenticLoop(baseOptions);

            expect(mockStripPreamble).toHaveBeenCalledWith('Preamble here\n## Stripped content');
            expect(result).toBe('## Stripped content');
        });
    });

    describe('tool execution loop', () => {
        it('executes tool and continues loop', async () => {
            // First call: LLM requests a tool
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({
                    content: [
                        {
                            type: 'tool_use',
                            id: 'tool-1',
                            name: 'query_database',
                            input: { query: 'SELECT 1' },
                        },
                    ],
                }),
            );

            // Tool execution
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({
                    content: [{ text: 'Query result: 1' }],
                }),
            );

            // Second call: LLM returns final response
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({
                    content: [{ type: 'text', text: 'The result is 1.' }],
                }),
            );

            const result = await runAgenticLoop(baseOptions);

            expect(result).toBe('The result is 1.');
            expect(mockApiFetch).toHaveBeenCalledTimes(3);
        });

        it('handles multiple tool calls in one response', async () => {
            // First call: LLM requests multiple tools
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({
                    content: [
                        { type: 'tool_use', id: 'tool-1', name: 'query_database', input: {} },
                        { type: 'tool_use', id: 'tool-2', name: 'get_schema_info', input: {} },
                    ],
                }),
            );

            // Tool executions
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({ content: [{ text: 'Result 1' }] }),
            );
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({ content: [{ text: 'Result 2' }] }),
            );

            // Final response
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({
                    content: [{ type: 'text', text: 'Combined analysis.' }],
                }),
            );

            const result = await runAgenticLoop(baseOptions);

            expect(result).toBe('Combined analysis.');
            expect(mockApiFetch).toHaveBeenCalledTimes(4);
        });

        it('handles tool execution errors gracefully', async () => {
            // First call: LLM requests a tool
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({
                    content: [
                        { type: 'tool_use', id: 'tool-1', name: 'query_database', input: {} },
                    ],
                }),
            );

            // Tool execution fails
            mockApiFetch.mockRejectedValueOnce(new Error('Tool failed'));

            // LLM handles error and returns response
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({
                    content: [{ type: 'text', text: 'Error occurred.' }],
                }),
            );

            const result = await runAgenticLoop(baseOptions);

            expect(result).toBe('Error occurred.');
        });

        it('handles tool returning isError flag', async () => {
            // First call: LLM requests a tool
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({
                    content: [
                        { type: 'tool_use', id: 'tool-1', name: 'query_database', input: {} },
                    ],
                }),
            );

            // Tool returns with isError
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({
                    isError: true,
                    content: [{ text: 'Database connection failed' }],
                }),
            );

            // Final response
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({
                    content: [{ type: 'text', text: 'I encountered an error.' }],
                }),
            );

            const result = await runAgenticLoop(baseOptions);

            expect(result).toBe('I encountered an error.');
        });

        it('handles tool with no data returned', async () => {
            // First call: LLM requests a tool
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({
                    content: [
                        { type: 'tool_use', id: 'tool-1', name: 'query_database', input: {} },
                    ],
                }),
            );

            // Tool returns empty content
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({ content: [] }),
            );

            // Final response
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({
                    content: [{ type: 'text', text: 'No data was returned.' }],
                }),
            );

            const result = await runAgenticLoop(baseOptions);

            expect(result).toBe('No data was returned.');
        });
    });

    describe('iteration limit', () => {
        it('throws error when max iterations exceeded', async () => {
            const options: AgenticLoopOptions = {
                ...baseOptions,
                maxIterations: 2,
            };

            // Both iterations return tool requests
            mockApiFetch.mockResolvedValue(
                createMockResponse({
                    content: [
                        { type: 'tool_use', id: 'tool-1', name: 'query_database', input: {} },
                    ],
                }),
            );

            await expect(runAgenticLoop(options)).rejects.toThrow(
                'Analysis exceeded maximum iterations',
            );
        });
    });

    describe('API error handling', () => {
        it('throws error when LLM API returns non-ok response', async () => {
            mockApiFetch.mockResolvedValueOnce({
                ok: false,
                text: vi.fn().mockResolvedValue('Internal server error'),
            });

            await expect(runAgenticLoop(baseOptions)).rejects.toThrow(
                'Analysis request failed: Internal server error',
            );
        });
    });

    describe('callbacks', () => {
        it('calls onActiveTools with tool names during execution', async () => {
            const onActiveTools = vi.fn();

            // First call: LLM requests a tool
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({
                    content: [
                        { type: 'tool_use', id: 'tool-1', name: 'query_database', input: {} },
                    ],
                }),
            );

            // Tool execution
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({ content: [{ text: 'Result' }] }),
            );

            // Final response
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({
                    content: [{ type: 'text', text: 'Done.' }],
                }),
            );

            await runAgenticLoop({ ...baseOptions, onActiveTools });

            expect(onActiveTools).toHaveBeenCalledWith(['Querying database']);
            expect(onActiveTools).toHaveBeenCalledWith([]);
        });

        it('calls onActiveTools with empty array on final response', async () => {
            const onActiveTools = vi.fn();

            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({
                    content: [{ type: 'text', text: 'Hello' }],
                }),
            );

            await runAgenticLoop({ ...baseOptions, onActiveTools });

            expect(onActiveTools).toHaveBeenCalledWith([]);
        });

        it('calls onProgress with single tool message', async () => {
            const onProgress = vi.fn();

            // First call: LLM requests a tool
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({
                    content: [
                        { type: 'tool_use', id: 'tool-1', name: 'query_database', input: {} },
                    ],
                }),
            );

            // Tool execution
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({ content: [{ text: 'Result' }] }),
            );

            // Final response
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({
                    content: [{ type: 'text', text: 'Done.' }],
                }),
            );

            await runAgenticLoop({ ...baseOptions, onProgress });

            expect(onProgress).toHaveBeenCalledWith('Querying database...');
            expect(onProgress).toHaveBeenCalledWith('Analyzing results...');
        });

        it('calls onProgress with multiple tools message', async () => {
            const onProgress = vi.fn();

            // First call: LLM requests multiple tools
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({
                    content: [
                        { type: 'tool_use', id: 'tool-1', name: 'query_database', input: {} },
                        { type: 'tool_use', id: 'tool-2', name: 'get_schema_info', input: {} },
                    ],
                }),
            );

            // Tool executions
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({ content: [{ text: 'Result 1' }] }),
            );
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({ content: [{ text: 'Result 2' }] }),
            );

            // Final response
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({
                    content: [{ type: 'text', text: 'Done.' }],
                }),
            );

            await runAgenticLoop({ ...baseOptions, onProgress });

            expect(onProgress).toHaveBeenCalledWith('Running 2 tools...');
        });

        it('deduplicates tool names in onActiveTools', async () => {
            const onActiveTools = vi.fn();

            // LLM requests same tool twice
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({
                    content: [
                        { type: 'tool_use', id: 'tool-1', name: 'query_database', input: {} },
                        { type: 'tool_use', id: 'tool-2', name: 'query_database', input: {} },
                    ],
                }),
            );

            // Tool executions
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({ content: [{ text: 'Result 1' }] }),
            );
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({ content: [{ text: 'Result 2' }] }),
            );

            // Final response
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({
                    content: [{ type: 'text', text: 'Done.' }],
                }),
            );

            await runAgenticLoop({ ...baseOptions, onActiveTools });

            // Should only have one unique tool name
            expect(onActiveTools).toHaveBeenCalledWith(['Querying database']);
        });
    });

    describe('message building', () => {
        it('sends correct initial request body', async () => {
            const tools = [
                {
                    name: 'test_tool',
                    description: 'A test tool',
                    inputSchema: { type: 'object', properties: {}, required: [] },
                },
            ];

            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({
                    content: [{ type: 'text', text: 'Response' }],
                }),
            );

            await runAgenticLoop({ ...baseOptions, tools });

            expect(mockApiFetch).toHaveBeenCalledWith('/api/v1/llm/chat', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    messages: baseOptions.messages,
                    tools,
                    system: baseOptions.systemPrompt,
                }),
            });
        });

        it('omits tools from request when tools array is empty', async () => {
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({
                    content: [{ type: 'text', text: 'Response' }],
                }),
            );

            await runAgenticLoop({ ...baseOptions, tools: [] });

            const call = mockApiFetch.mock.calls[0];
            const body = JSON.parse(call[1].body as string);

            expect(body.tools).toBeUndefined();
        });

        it('handles tool_use with missing id', async () => {
            // First call: LLM requests a tool without id
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({
                    content: [
                        { type: 'tool_use', name: 'query_database', input: {} },
                    ],
                }),
            );

            // Tool execution
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({ content: [{ text: 'Result' }] }),
            );

            // Final response
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({
                    content: [{ type: 'text', text: 'Done.' }],
                }),
            );

            const result = await runAgenticLoop(baseOptions);

            expect(result).toBe('Done.');
        });

        it('handles tool_use with missing name', async () => {
            // First call: LLM requests a tool without name
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({
                    content: [
                        { type: 'tool_use', id: 'tool-1', input: {} },
                    ],
                }),
            );

            // Tool execution
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({ content: [{ text: 'Result' }] }),
            );

            // Final response
            mockApiFetch.mockResolvedValueOnce(
                createMockResponse({
                    content: [{ type: 'text', text: 'Done.' }],
                }),
            );

            const result = await runAgenticLoop(baseOptions);

            expect(result).toBe('Done.');
        });
    });
});
