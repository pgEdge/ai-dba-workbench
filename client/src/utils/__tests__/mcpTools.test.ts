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
import { getKnowledgebaseTool } from '../mcpTools';

// Mock the apiClient module
vi.mock('../apiClient', () => ({
    apiFetch: vi.fn(),
}));

import { apiFetch } from '../apiClient';

const mockApiFetch = apiFetch as ReturnType<typeof vi.fn>;

describe('getKnowledgebaseTool', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    describe('successful responses', () => {
        it('returns the search_knowledgebase tool when found', async () => {
            const mockResponse = {
                ok: true,
                json: vi.fn().mockResolvedValue({
                    tools: [
                        {
                            name: 'search_knowledgebase',
                            description: 'Search the knowledge base',
                            inputSchema: {
                                type: 'object',
                                properties: {
                                    query: { type: 'string', description: 'Search query' },
                                },
                                required: ['query'],
                            },
                        },
                        {
                            name: 'other_tool',
                            description: 'Another tool',
                            inputSchema: {
                                type: 'object',
                                properties: {},
                            },
                        },
                    ],
                }),
            };

            mockApiFetch.mockResolvedValue(mockResponse);

            const result = await getKnowledgebaseTool();

            expect(result).not.toBeNull();
            expect(result?.name).toBe('search_knowledgebase');
            expect(result?.description).toBe('Search the knowledge base');
            expect(result?.inputSchema.type).toBe('object');
            expect(result?.inputSchema.properties).toHaveProperty('query');
            expect(result?.inputSchema.required).toEqual(['query']);
        });

        it('returns correct inputSchema structure', async () => {
            const mockResponse = {
                ok: true,
                json: vi.fn().mockResolvedValue({
                    tools: [
                        {
                            name: 'search_knowledgebase',
                            description: 'Test description',
                            inputSchema: {
                                type: 'object',
                                properties: {
                                    query: { type: 'string', description: 'The query' },
                                    limit: { type: 'number', description: 'Max results' },
                                },
                                required: ['query'],
                            },
                        },
                    ],
                }),
            };

            mockApiFetch.mockResolvedValue(mockResponse);

            const result = await getKnowledgebaseTool();

            expect(result?.inputSchema.properties).toEqual({
                query: { type: 'string', description: 'The query' },
                limit: { type: 'number', description: 'Max results' },
            });
        });

        it('defaults required to empty array when not provided', async () => {
            const mockResponse = {
                ok: true,
                json: vi.fn().mockResolvedValue({
                    tools: [
                        {
                            name: 'search_knowledgebase',
                            description: 'No required fields',
                            inputSchema: {
                                type: 'object',
                                properties: {
                                    optional: { type: 'string' },
                                },
                                // required is undefined
                            },
                        },
                    ],
                }),
            };

            mockApiFetch.mockResolvedValue(mockResponse);

            const result = await getKnowledgebaseTool();

            expect(result?.inputSchema.required).toEqual([]);
        });
    });

    describe('tool not found', () => {
        it('returns null when search_knowledgebase is not in the list', async () => {
            const mockResponse = {
                ok: true,
                json: vi.fn().mockResolvedValue({
                    tools: [
                        {
                            name: 'other_tool',
                            description: 'Not the one we want',
                            inputSchema: { type: 'object', properties: {} },
                        },
                    ],
                }),
            };

            mockApiFetch.mockResolvedValue(mockResponse);

            const result = await getKnowledgebaseTool();

            expect(result).toBeNull();
        });

        it('returns null when tools array is empty', async () => {
            const mockResponse = {
                ok: true,
                json: vi.fn().mockResolvedValue({ tools: [] }),
            };

            mockApiFetch.mockResolvedValue(mockResponse);

            const result = await getKnowledgebaseTool();

            expect(result).toBeNull();
        });
    });

    describe('error handling', () => {
        it('returns null when response is not ok', async () => {
            const mockResponse = {
                ok: false,
                status: 404,
            };

            mockApiFetch.mockResolvedValue(mockResponse);

            const result = await getKnowledgebaseTool();

            expect(result).toBeNull();
        });

        it('returns null when fetch throws an error', async () => {
            mockApiFetch.mockRejectedValue(new Error('Network error'));

            const result = await getKnowledgebaseTool();

            expect(result).toBeNull();
        });

        it('returns null on server error', async () => {
            const mockResponse = {
                ok: false,
                status: 500,
            };

            mockApiFetch.mockResolvedValue(mockResponse);

            const result = await getKnowledgebaseTool();

            expect(result).toBeNull();
        });

        it('returns null when JSON parsing fails', async () => {
            const mockResponse = {
                ok: true,
                json: vi.fn().mockRejectedValue(new Error('Invalid JSON')),
            };

            mockApiFetch.mockResolvedValue(mockResponse);

            const result = await getKnowledgebaseTool();

            expect(result).toBeNull();
        });
    });

    describe('API call verification', () => {
        it('calls apiFetch with correct endpoint', async () => {
            const mockResponse = {
                ok: true,
                json: vi.fn().mockResolvedValue({ tools: [] }),
            };

            mockApiFetch.mockResolvedValue(mockResponse);

            await getKnowledgebaseTool();

            expect(mockApiFetch).toHaveBeenCalledWith('/api/v1/mcp/tools');
            expect(mockApiFetch).toHaveBeenCalledTimes(1);
        });
    });
});
