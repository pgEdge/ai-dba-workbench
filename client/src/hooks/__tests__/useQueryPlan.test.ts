/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import { useQueryPlan } from '../useQueryPlan';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const mockApiFetch = vi.fn();

vi.mock('../../utils/apiClient', () => ({
    apiFetch: (...args: unknown[]) => mockApiFetch(...args),
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Counter to generate unique cache keys per test. */
let testCounter = 0;

/**
 * Build a mock response matching the QueryResponse shape
 * returned by POST /api/v1/connections/:id/query.
 */
function makeQueryResponse(
    rows: string[][],
    query: string = 'EXPLAIN SELECT 1',
) {
    return {
        ok: true,
        text: vi.fn().mockResolvedValue(''),
        json: vi.fn().mockResolvedValue({
            results: [
                {
                    columns: ['QUERY PLAN'],
                    rows,
                    row_count: rows.length,
                    truncated: false,
                    query,
                },
            ],
            total_statements: 1,
        }),
    };
}

function makeErrorResponse(
    errorText: string = 'Internal Server Error',
) {
    return {
        ok: false,
        text: vi.fn().mockResolvedValue(errorText),
        json: vi.fn(),
    };
}

function makeQueryErrorResponse(errorMessage: string) {
    return {
        ok: true,
        text: vi.fn().mockResolvedValue(''),
        json: vi.fn().mockResolvedValue({
            results: [
                {
                    columns: [],
                    rows: [],
                    row_count: 0,
                    truncated: false,
                    query: 'EXPLAIN SELECT 1',
                    error: errorMessage,
                },
            ],
            total_statements: 1,
        }),
    };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('useQueryPlan', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        testCounter++;
    });

    it('returns null plans and no error initially', () => {
        const { result } = renderHook(() =>
            useQueryPlan(`SELECT ${testCounter}`, 1, 'testdb'),
        );

        expect(result.current.textPlan).toBeNull();
        expect(result.current.jsonPlan).toBeNull();
        expect(result.current.loading).toBe(false);
        expect(result.current.error).toBeNull();
        expect(typeof result.current.fetch).toBe('function');
    });

    it('fetch triggers two parallel API calls', async () => {
        const textResponse = makeQueryResponse(
            [['Seq Scan on users']],
            'EXPLAIN SELECT 1',
        );
        const jsonStr = JSON.stringify(
            [{ Plan: { 'Node Type': 'Seq Scan' } }],
        );
        const jsonResponse = makeQueryResponse(
            [[jsonStr]],
            'EXPLAIN (FORMAT JSON) SELECT 1',
        );

        mockApiFetch
            .mockResolvedValueOnce(textResponse)
            .mockResolvedValueOnce(jsonResponse);

        const { result } = renderHook(() =>
            useQueryPlan(`SELECT ${testCounter}`, 1, 'testdb'),
        );

        await act(async () => {
            result.current.fetch();
        });

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(2);
        });

        // Verify the URLs contain the connection ID
        const urls = mockApiFetch.mock.calls.map(
            (call: unknown[]) => call[0],
        );
        expect(urls[0]).toBe('/api/v1/connections/1/query');
        expect(urls[1]).toBe('/api/v1/connections/1/query');

        // Verify request bodies contain EXPLAIN queries
        const bodies = mockApiFetch.mock.calls.map(
            (call: unknown[]) =>
                JSON.parse(
                    (call[1] as { body: string }).body,
                ),
        );
        const queries = bodies.map(
            (b: { query: string }) => b.query,
        );
        expect(queries).toContain(
            `EXPLAIN SELECT ${testCounter}`,
        );
        expect(queries).toContain(
            `EXPLAIN (VERBOSE, FORMAT JSON) SELECT ${testCounter}`,
        );
    });

    it('successfully parses text plan from response', async () => {
        const planText =
            'Seq Scan on users  (cost=0.00..35.50'
            + ' rows=2550 width=4)';
        const textResponse = makeQueryResponse(
            [[planText]],
        );
        const jsonStr = JSON.stringify(
            [{ Plan: { 'Node Type': 'Seq Scan' } }],
        );
        const jsonResponse = makeQueryResponse(
            [[jsonStr]],
        );

        mockApiFetch
            .mockResolvedValueOnce(textResponse)
            .mockResolvedValueOnce(jsonResponse);

        const { result } = renderHook(() =>
            useQueryPlan(`SELECT ${testCounter}`, 1, 'testdb'),
        );

        await act(async () => {
            result.current.fetch();
        });

        await waitFor(() => {
            expect(result.current.textPlan).toBe(planText);
        });
    });

    it('successfully parses JSON plan and unwraps Plan nodes', async () => {
        const planNode = {
            'Node Type': 'Seq Scan',
            'Total Cost': 35.5,
            'Startup Cost': 0.0,
            'Plan Rows': 2550,
            'Plan Width': 4,
        };
        const textResponse = makeQueryResponse(
            [['Seq Scan on users']],
        );
        const jsonResponse = makeQueryResponse(
            [[JSON.stringify([{ Plan: planNode }])]],
        );

        mockApiFetch
            .mockResolvedValueOnce(textResponse)
            .mockResolvedValueOnce(jsonResponse);

        const { result } = renderHook(() =>
            useQueryPlan(`SELECT ${testCounter}`, 1, 'testdb'),
        );

        await act(async () => {
            result.current.fetch();
        });

        await waitFor(() => {
            expect(result.current.jsonPlan).not.toBeNull();
        });

        expect(result.current.jsonPlan).toHaveLength(1);
        expect(
            result.current.jsonPlan![0]['Node Type'],
        ).toBe('Seq Scan');
        expect(
            result.current.jsonPlan![0]['Total Cost'],
        ).toBe(35.5);
    });

    it('sets error when both requests fail', async () => {
        mockApiFetch
            .mockResolvedValueOnce(
                makeErrorResponse('text error'),
            )
            .mockResolvedValueOnce(
                makeErrorResponse('json error'),
            );

        const { result } = renderHook(() =>
            useQueryPlan(`SELECT ${testCounter}`, 1, 'testdb'),
        );

        await act(async () => {
            result.current.fetch();
        });

        await waitFor(() => {
            expect(result.current.error).not.toBeNull();
        });

        expect(result.current.textPlan).toBeNull();
        expect(result.current.jsonPlan).toBeNull();
        expect(result.current.error).toContain('Text plan');
        expect(result.current.error).toContain('JSON plan');
    });

    it('provides partial results when text succeeds but JSON fails', async () => {
        const planText = 'Seq Scan on users';
        const textResponse = makeQueryResponse(
            [[planText]],
        );

        mockApiFetch
            .mockResolvedValueOnce(textResponse)
            .mockResolvedValueOnce(
                makeErrorResponse('json error'),
            );

        const { result } = renderHook(() =>
            useQueryPlan(`SELECT ${testCounter}`, 1, 'testdb'),
        );

        await act(async () => {
            result.current.fetch();
        });

        await waitFor(() => {
            expect(result.current.textPlan).toBe(planText);
        });

        expect(result.current.jsonPlan).toBeNull();
        expect(result.current.error).toBeNull();
    });

    it('serves from cache on second fetch', async () => {
        const planText = 'Seq Scan on users';
        const textResponse = makeQueryResponse(
            [[planText]],
        );
        const jsonStr = JSON.stringify(
            [{ Plan: { 'Node Type': 'Seq Scan' } }],
        );
        const jsonResponse = makeQueryResponse(
            [[jsonStr]],
        );

        mockApiFetch
            .mockResolvedValueOnce(textResponse)
            .mockResolvedValueOnce(jsonResponse);

        const query = `SELECT cache_${testCounter}`;
        const { result } = renderHook(() =>
            useQueryPlan(query, 1, 'testdb'),
        );

        await act(async () => {
            result.current.fetch();
        });

        await waitFor(() => {
            expect(result.current.textPlan).toBe(planText);
        });

        expect(mockApiFetch).toHaveBeenCalledTimes(2);

        // Second fetch should serve from cache
        await act(async () => {
            result.current.fetch();
        });

        // No new API calls
        expect(mockApiFetch).toHaveBeenCalledTimes(2);
        expect(result.current.textPlan).toBe(planText);
    });

    it('handles empty response rows gracefully', async () => {
        const emptyResponse = makeQueryResponse([]);
        mockApiFetch
            .mockResolvedValueOnce(emptyResponse)
            .mockResolvedValueOnce(emptyResponse);

        const { result } = renderHook(() =>
            useQueryPlan(`SELECT ${testCounter}`, 1, 'testdb'),
        );

        await act(async () => {
            result.current.fetch();
        });

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });

        // Empty rows join to empty string for text plan;
        // the hook treats it as a valid (albeit empty) result.
        // JSON parse of empty string fails, so jsonPlan is null.
        expect(result.current.textPlan).toBe('');
        expect(result.current.jsonPlan).toBeNull();
        // No error because text plan succeeded (partial result)
        expect(result.current.error).toBeNull();
    });

    it('throws when results contain an error field', async () => {
        mockApiFetch
            .mockResolvedValueOnce(
                makeQueryErrorResponse('relation "foo" does not exist'),
            )
            .mockResolvedValueOnce(
                makeQueryErrorResponse('relation "foo" does not exist'),
            );

        const { result } = renderHook(() =>
            useQueryPlan(`SELECT ${testCounter}`, 1, 'testdb'),
        );

        await act(async () => {
            result.current.fetch();
        });

        await waitFor(() => {
            expect(result.current.error).not.toBeNull();
        });

        expect(result.current.textPlan).toBeNull();
        expect(result.current.jsonPlan).toBeNull();
        expect(result.current.error).toContain(
            'relation "foo" does not exist',
        );
    });

    it('uses GENERIC_PLAN with VERBOSE for parameterized queries', async () => {
        const query = `SELECT $1 AS val_${testCounter}`;
        const textResponse = makeQueryResponse(
            [['Seq Scan']],
        );
        const jsonStr = JSON.stringify(
            [{ Plan: { 'Node Type': 'Seq Scan' } }],
        );
        const jsonResponse = makeQueryResponse(
            [[jsonStr]],
        );

        mockApiFetch
            .mockResolvedValueOnce(textResponse)
            .mockResolvedValueOnce(jsonResponse);

        const { result } = renderHook(() =>
            useQueryPlan(query, 1, 'testdb'),
        );

        await act(async () => {
            result.current.fetch();
        });

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(2);
        });

        const bodies = mockApiFetch.mock.calls.map(
            (call: unknown[]) =>
                JSON.parse(
                    (call[1] as { body: string }).body,
                ),
        );
        const queries = bodies.map(
            (b: { query: string }) => b.query,
        );

        // The JSON query should contain both VERBOSE and
        // GENERIC_PLAN options.
        expect(queries).toContain(
            `EXPLAIN (VERBOSE, GENERIC_PLAN, FORMAT JSON) ${query}`,
        );

        // The text query should contain GENERIC_PLAN.
        expect(queries).toContain(
            `EXPLAIN (GENERIC_PLAN) ${query}`,
        );
    });

    it('sends database_name in the request body', async () => {
        const textResponse = makeQueryResponse(
            [['Seq Scan']],
        );
        const jsonStr = JSON.stringify(
            [{ Plan: { 'Node Type': 'Seq Scan' } }],
        );
        const jsonResponse = makeQueryResponse(
            [[jsonStr]],
        );

        mockApiFetch
            .mockResolvedValueOnce(textResponse)
            .mockResolvedValueOnce(jsonResponse);

        const { result } = renderHook(() =>
            useQueryPlan(
                `SELECT ${testCounter}`, 1, 'my_database',
            ),
        );

        await act(async () => {
            result.current.fetch();
        });

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(2);
        });

        const bodies = mockApiFetch.mock.calls.map(
            (call: unknown[]) =>
                JSON.parse(
                    (call[1] as { body: string }).body,
                ),
        );
        for (const body of bodies) {
            expect(body.database_name).toBe('my_database');
        }
    });
});
