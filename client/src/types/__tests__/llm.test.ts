/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect } from 'vitest';
import { normaliseTools } from '../llm';

describe('normaliseTools', () => {
    it('renames inputSchema to input_schema for each tool', () => {
        const tools = [
            {
                name: 'list_connections',
                description: 'List database connections',
                inputSchema: { type: 'object', properties: {}, required: [] },
            },
        ];

        const result = normaliseTools(tools);

        expect(result).toEqual([
            {
                name: 'list_connections',
                description: 'List database connections',
                input_schema: {
                    type: 'object',
                    properties: {},
                    required: [],
                },
            },
        ]);
    });

    it('does not carry an inputSchema key on the returned objects', () => {
        const tools = [
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

        const result = normaliseTools(tools);

        expect(result[0]).not.toHaveProperty('inputSchema');
        expect(result[0]).toHaveProperty('input_schema');
    });

    it('preserves name and description unchanged', () => {
        const tools = [
            {
                name: 'search_knowledgebase',
                description: 'Search the knowledgebase',
                inputSchema: { type: 'object', properties: {}, required: [] },
            },
        ];

        const [result] = normaliseTools(tools);

        expect(result.name).toBe('search_knowledgebase');
        expect(result.description).toBe('Search the knowledgebase');
    });

    it('handles multiple tools, preserving order', () => {
        const tools = [
            {
                name: 'tool_a',
                description: 'First tool',
                inputSchema: { type: 'object', properties: {}, required: [] },
            },
            {
                name: 'tool_b',
                description: 'Second tool',
                inputSchema: { type: 'object', properties: {}, required: [] },
            },
        ];

        const result = normaliseTools(tools);

        expect(result.map(t => t.name)).toEqual(['tool_a', 'tool_b']);
    });

    it('returns an empty array when given an empty array', () => {
        expect(normaliseTools([])).toEqual([]);
    });

    it('does not mutate the input array or its objects', () => {
        const tools = [
            {
                name: 'list_connections',
                description: 'List database connections',
                inputSchema: { type: 'object', properties: {}, required: [] },
            },
        ];
        const original = JSON.parse(JSON.stringify(tools));

        const result = normaliseTools(tools);

        expect(tools).toEqual(original);
        expect(result).not.toBe(tools);
        expect(result[0]).not.toBe(tools[0]);
        expect(tools[0]).not.toHaveProperty('input_schema');
    });

    it('preserves an arbitrary inputSchema shape verbatim as input_schema', () => {
        const arbitrarySchema = {
            type: 'object',
            properties: { foo: { type: 'string' } },
            required: ['foo'],
            additionalProperties: false,
        };
        const tools = [
            {
                name: 'custom_tool',
                description: 'A custom tool',
                inputSchema: arbitrarySchema,
            },
        ];

        const [result] = normaliseTools(tools);

        expect(result.input_schema).toEqual(arbitrarySchema);
    });
});
