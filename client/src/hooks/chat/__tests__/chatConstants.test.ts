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
import {
    SYSTEM_PROMPT,
    CHAT_TOOLS,
    COMPACTION_TOKEN_THRESHOLD,
    COMPACTION_MAX_TOKENS,
    COMPACTION_RECENT_WINDOW,
    INPUT_HISTORY_KEY,
    INPUT_HISTORY_MAX,
} from '../chatConstants';

describe('chatConstants', () => {
    describe('SYSTEM_PROMPT', () => {
        it('is a non-empty string', () => {
            expect(typeof SYSTEM_PROMPT).toBe('string');
            expect(SYSTEM_PROMPT.length).toBeGreaterThan(0);
        });

        it('contains the expected persona name', () => {
            expect(SYSTEM_PROMPT).toContain('Ellie');
        });

        it('contains key instructions about tool usage', () => {
            expect(SYSTEM_PROMPT).toContain('TOOL USAGE GUIDELINES');
            expect(SYSTEM_PROMPT).toContain('list_connections');
            expect(SYSTEM_PROMPT).toContain('query_database');
        });

        it('contains security instructions', () => {
            expect(SYSTEM_PROMPT).toContain('CRITICAL - Security and identity');
        });
    });

    describe('CHAT_TOOLS', () => {
        it('is an array with the expected number of tools', () => {
            expect(Array.isArray(CHAT_TOOLS)).toBe(true);
            expect(CHAT_TOOLS.length).toBe(11);
        });

        it('each tool has required properties', () => {
            for (const tool of CHAT_TOOLS) {
                expect(tool).toHaveProperty('name');
                expect(tool).toHaveProperty('description');
                expect(tool).toHaveProperty('inputSchema');

                expect(typeof tool.name).toBe('string');
                expect(tool.name.length).toBeGreaterThan(0);

                expect(typeof tool.description).toBe('string');
                expect(tool.description.length).toBeGreaterThan(0);

                expect(typeof tool.inputSchema).toBe('object');
                expect(tool.inputSchema).toHaveProperty('type');
                expect(tool.inputSchema).toHaveProperty('properties');
                expect(tool.inputSchema).toHaveProperty('required');
            }
        });

        it('contains the list_connections tool', () => {
            const listConnections = CHAT_TOOLS.find(
                t => t.name === 'list_connections'
            );
            expect(listConnections).toBeDefined();
            expect(listConnections?.description).toContain('database connections');
            expect(listConnections?.inputSchema.required).toEqual([]);
        });

        it('contains the query_database tool with required query parameter', () => {
            const queryDb = CHAT_TOOLS.find(t => t.name === 'query_database');
            expect(queryDb).toBeDefined();
            expect(queryDb?.inputSchema.required).toContain('query');
            expect(queryDb?.inputSchema.properties).toHaveProperty('query');
            expect(queryDb?.inputSchema.properties).toHaveProperty('limit');
            expect(queryDb?.inputSchema.properties).toHaveProperty('connection_id');
        });

        it('contains the query_metrics tool', () => {
            const queryMetrics = CHAT_TOOLS.find(t => t.name === 'query_metrics');
            expect(queryMetrics).toBeDefined();
            expect(queryMetrics?.inputSchema.required).toContain('probe_name');
        });

        it('contains the search_knowledgebase tool', () => {
            const searchKb = CHAT_TOOLS.find(
                t => t.name === 'search_knowledgebase'
            );
            expect(searchKb).toBeDefined();
            expect(searchKb?.inputSchema.required).toContain('query');
        });

        it('contains the get_schema_info tool', () => {
            const schemaInfo = CHAT_TOOLS.find(t => t.name === 'get_schema_info');
            expect(schemaInfo).toBeDefined();
            expect(schemaInfo?.inputSchema.required).toEqual([]);
        });

        it('contains the execute_explain tool', () => {
            const explain = CHAT_TOOLS.find(t => t.name === 'execute_explain');
            expect(explain).toBeDefined();
            expect(explain?.inputSchema.required).toContain('query');
        });

        it('contains the list_probes tool', () => {
            const listProbes = CHAT_TOOLS.find(t => t.name === 'list_probes');
            expect(listProbes).toBeDefined();
            expect(listProbes?.inputSchema.required).toEqual([]);
        });

        it('contains the describe_probe tool', () => {
            const describeProbe = CHAT_TOOLS.find(t => t.name === 'describe_probe');
            expect(describeProbe).toBeDefined();
            expect(describeProbe?.inputSchema.required).toContain('probe_name');
        });

        it('contains the get_alert_history tool', () => {
            const alertHistory = CHAT_TOOLS.find(
                t => t.name === 'get_alert_history'
            );
            expect(alertHistory).toBeDefined();
            expect(alertHistory?.inputSchema.required).toContain('connection_id');
        });

        it('contains the get_alert_rules tool', () => {
            const alertRules = CHAT_TOOLS.find(t => t.name === 'get_alert_rules');
            expect(alertRules).toBeDefined();
            expect(alertRules?.inputSchema.required).toEqual([]);
        });

        it('contains the query_datastore tool', () => {
            const queryDatastore = CHAT_TOOLS.find(
                t => t.name === 'query_datastore'
            );
            expect(queryDatastore).toBeDefined();
            expect(queryDatastore?.inputSchema.required).toContain('query');
        });

        it('each tool inputSchema has type "object"', () => {
            for (const tool of CHAT_TOOLS) {
                expect(tool.inputSchema.type).toBe('object');
            }
        });
    });

    describe('compaction thresholds', () => {
        it('COMPACTION_TOKEN_THRESHOLD is a positive number', () => {
            expect(typeof COMPACTION_TOKEN_THRESHOLD).toBe('number');
            expect(COMPACTION_TOKEN_THRESHOLD).toBeGreaterThan(0);
        });

        it('COMPACTION_MAX_TOKENS is a positive number', () => {
            expect(typeof COMPACTION_MAX_TOKENS).toBe('number');
            expect(COMPACTION_MAX_TOKENS).toBeGreaterThan(0);
        });

        it('COMPACTION_MAX_TOKENS is greater than COMPACTION_TOKEN_THRESHOLD', () => {
            expect(COMPACTION_MAX_TOKENS).toBeGreaterThan(
                COMPACTION_TOKEN_THRESHOLD
            );
        });

        it('COMPACTION_RECENT_WINDOW is a positive number', () => {
            expect(typeof COMPACTION_RECENT_WINDOW).toBe('number');
            expect(COMPACTION_RECENT_WINDOW).toBeGreaterThan(0);
        });
    });

    describe('input history configuration', () => {
        it('INPUT_HISTORY_KEY is defined as expected string', () => {
            expect(typeof INPUT_HISTORY_KEY).toBe('string');
            expect(INPUT_HISTORY_KEY).toBe('chat-input-history');
        });

        it('INPUT_HISTORY_MAX is a positive number', () => {
            expect(typeof INPUT_HISTORY_MAX).toBe('number');
            expect(INPUT_HISTORY_MAX).toBeGreaterThan(0);
        });

        it('INPUT_HISTORY_MAX has a reasonable limit', () => {
            expect(INPUT_HISTORY_MAX).toBe(50);
        });
    });
});
