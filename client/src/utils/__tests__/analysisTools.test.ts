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
    TOOL_TEST_QUERY,
    TOOL_QUERY_METRICS,
    TOOL_GET_METRIC_BASELINES,
    TOOL_GET_ALERT_HISTORY,
    TOOL_GET_ALERT_RULES,
    TOOL_QUERY_DATABASE,
    TOOL_GET_SCHEMA_INFO,
    TOOL_LIST_PROBES,
    TOOL_DESCRIBE_PROBE,
    TOOL_GET_BLACKOUTS,
    SERVER_ANALYSIS_TOOLS,
    ALERT_ANALYSIS_TOOLS,
} from '../analysisTools';

// ---------------------------------------------------------------------------
// Individual tool constants
// ---------------------------------------------------------------------------

describe('TOOL_TEST_QUERY', () => {
    it('has the name "test_query"', () => {
        expect(TOOL_TEST_QUERY.name).toBe('test_query');
    });

    it('requires connection_id and query', () => {
        expect(TOOL_TEST_QUERY.inputSchema.required).toEqual(
            expect.arrayContaining(['connection_id', 'query']),
        );
    });
});

// ---------------------------------------------------------------------------
// SERVER_ANALYSIS_TOOLS
// ---------------------------------------------------------------------------

describe('SERVER_ANALYSIS_TOOLS', () => {
    it('contains TOOL_TEST_QUERY', () => {
        expect(SERVER_ANALYSIS_TOOLS).toContain(TOOL_TEST_QUERY);
    });

    it('contains core query and metrics tools', () => {
        expect(SERVER_ANALYSIS_TOOLS).toContain(TOOL_QUERY_METRICS);
        expect(SERVER_ANALYSIS_TOOLS).toContain(TOOL_GET_METRIC_BASELINES);
        expect(SERVER_ANALYSIS_TOOLS).toContain(TOOL_QUERY_DATABASE);
        expect(SERVER_ANALYSIS_TOOLS).toContain(TOOL_GET_SCHEMA_INFO);
    });

    it('contains alert tools', () => {
        expect(SERVER_ANALYSIS_TOOLS).toContain(TOOL_GET_ALERT_HISTORY);
        expect(SERVER_ANALYSIS_TOOLS).toContain(TOOL_GET_ALERT_RULES);
    });

    it('contains probe and blackout tools', () => {
        expect(SERVER_ANALYSIS_TOOLS).toContain(TOOL_LIST_PROBES);
        expect(SERVER_ANALYSIS_TOOLS).toContain(TOOL_DESCRIBE_PROBE);
        expect(SERVER_ANALYSIS_TOOLS).toContain(TOOL_GET_BLACKOUTS);
    });

    it('includes exactly 10 tools', () => {
        expect(SERVER_ANALYSIS_TOOLS).toHaveLength(10);
    });
});

// ---------------------------------------------------------------------------
// ALERT_ANALYSIS_TOOLS
// ---------------------------------------------------------------------------

describe('ALERT_ANALYSIS_TOOLS', () => {
    it('contains TOOL_TEST_QUERY', () => {
        expect(ALERT_ANALYSIS_TOOLS).toContain(TOOL_TEST_QUERY);
    });

    it('contains alert-specific tools', () => {
        expect(ALERT_ANALYSIS_TOOLS).toContain(TOOL_GET_ALERT_HISTORY);
        expect(ALERT_ANALYSIS_TOOLS).toContain(TOOL_GET_ALERT_RULES);
        expect(ALERT_ANALYSIS_TOOLS).toContain(TOOL_GET_METRIC_BASELINES);
        expect(ALERT_ANALYSIS_TOOLS).toContain(TOOL_QUERY_METRICS);
        expect(ALERT_ANALYSIS_TOOLS).toContain(TOOL_GET_BLACKOUTS);
    });

    it('includes exactly 6 tools', () => {
        expect(ALERT_ANALYSIS_TOOLS).toHaveLength(6);
    });

    it('is a subset of SERVER_ANALYSIS_TOOLS', () => {
        for (const tool of ALERT_ANALYSIS_TOOLS) {
            expect(SERVER_ANALYSIS_TOOLS).toContain(tool);
        }
    });
});
