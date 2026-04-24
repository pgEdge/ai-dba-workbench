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
import { useQueryOverview, type QueryOverviewInput } from '../useQueryOverview';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const mockSetAnalysis = vi.fn();
const mockSetLoading = vi.fn();
const mockSetError = vi.fn();
const mockSetProgressMessage = vi.fn();
const mockSetActiveTools = vi.fn();
const mockReset = vi.fn();

let mockAnalysis: string | null = null;
let mockLoading = false;
let mockError: string | null = null;

vi.mock('../useAnalysisState', () => ({
    useAnalysisState: () => ({
        state: {
            analysis: mockAnalysis,
            loading: mockLoading,
            error: mockError,
            progressMessage: '',
            activeTools: [],
        },
        setAnalysis: mockSetAnalysis,
        setLoading: mockSetLoading,
        setError: mockSetError,
        setProgressMessage: mockSetProgressMessage,
        setActiveTools: mockSetActiveTools,
        reset: mockReset,
    }),
}));

const mockApiFetch = vi.fn();

vi.mock('../../utils/apiClient', () => ({
    apiFetch: (...args: unknown[]) => mockApiFetch(...args),
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Counter to generate unique cache keys per test. */
let testCounter = 0;

function makeInput(overrides: Partial<QueryOverviewInput> = {}): QueryOverviewInput {
    return {
        queryText: 'SELECT * FROM users WHERE id = $1',
        queryId: `q-${testCounter}`,
        calls: 1000,
        totalExecTime: 5000,
        meanExecTime: 5.0,
        rows: 1000,
        sharedBlksHit: 900,
        sharedBlksRead: 100,
        connectionId: 1,
        databaseName: 'testdb',
        ...overrides,
    };
}

function makeSuccessResponse(text = 'Query appears healthy.') {
    return {
        ok: true,
        text: vi.fn().mockResolvedValue(''),
        json: vi.fn().mockResolvedValue({
            content: [{ type: 'text', text }],
        }),
    };
}

function makeErrorResponse(errorText = 'Internal Server Error') {
    return {
        ok: false,
        text: vi.fn().mockResolvedValue(errorText),
        json: vi.fn(),
    };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('useQueryOverview', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        testCounter++;
        mockAnalysis = null;
        mockLoading = false;
        mockError = null;
        mockApiFetch.mockResolvedValue(makeSuccessResponse());
    });

    it('returns null summary when input is null', () => {
        const { result } = renderHook(() => useQueryOverview(null));

        expect(result.current.summary).toBeNull();
        expect(result.current.loading).toBe(false);
        expect(result.current.error).toBeNull();
        expect(result.current.generatedAt).toBeNull();
    });

    it('triggers API call when input is provided', async () => {
        const input = makeInput();

        renderHook(() => useQueryOverview(input));

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(1);
        });

        expect(mockApiFetch).toHaveBeenCalledWith(
            '/api/v1/llm/chat',
            expect.objectContaining({
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
            }),
        );
    });

    it('sends system prompt and user message in the request body', async () => {
        const input = makeInput();

        renderHook(() => useQueryOverview(input));

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(1);
        });

        const callArgs = mockApiFetch.mock.calls[0];
        const body = JSON.parse(callArgs[1].body);

        expect(body.system).toContain('PostgreSQL expert');
        expect(body.messages).toHaveLength(1);
        expect(body.messages[0].role).toBe('user');
        expect(body.messages[0].content).toContain('SELECT * FROM users');
    });

    it('calls setAnalysis with text from LLM response on success', async () => {
        const summaryText = 'Query appears healthy and well-optimized.';
        mockApiFetch.mockResolvedValue(makeSuccessResponse(summaryText));

        renderHook(() => useQueryOverview(makeInput()));

        await waitFor(() => {
            expect(mockSetAnalysis).toHaveBeenCalledWith(summaryText);
        });
    });

    it('calls setLoading with true then false during API call', async () => {
        renderHook(() => useQueryOverview(makeInput()));

        await waitFor(() => {
            expect(mockSetLoading).toHaveBeenCalledWith(true);
        });

        await waitFor(() => {
            expect(mockSetLoading).toHaveBeenCalledWith(false);
        });
    });

    it('calls setError on API failure', async () => {
        mockApiFetch.mockResolvedValue(
            makeErrorResponse('Service Unavailable'),
        );

        renderHook(() => useQueryOverview(makeInput()));

        await waitFor(() => {
            expect(mockSetError).toHaveBeenCalledWith(
                expect.stringContaining('Service Unavailable'),
            );
        });
    });

    it('calls setError when apiFetch throws', async () => {
        mockApiFetch.mockRejectedValue(new Error('Network failure'));

        renderHook(() => useQueryOverview(makeInput()));

        await waitFor(() => {
            expect(mockSetError).toHaveBeenCalledWith('Network failure');
        });
    });

    it('does not trigger a second API call for the same input', async () => {
        const input = makeInput();
        const summaryText = 'Cached summary.';
        mockApiFetch.mockResolvedValue(makeSuccessResponse(summaryText));

        const { unmount } = renderHook(() => useQueryOverview(input));

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(1);
        });

        unmount();

        // Re-render with the same input; the cache should serve the result
        renderHook(() => useQueryOverview(input));

        // Should still only have one API call since the cache is warm
        expect(mockApiFetch).toHaveBeenCalledTimes(1);

        // The cached summary should have been set
        await waitFor(() => {
            expect(mockSetAnalysis).toHaveBeenCalledWith(summaryText);
        });
    });

    it('triggers a new API call when input identifiers change', async () => {
        const input1 = makeInput({ queryId: `change-q1-${testCounter}` });
        const input2 = makeInput({ queryId: `change-q2-${testCounter}` });
        mockApiFetch.mockResolvedValue(makeSuccessResponse());

        const { rerender } = renderHook(
            ({ input }: { input: QueryOverviewInput }) =>
                useQueryOverview(input),
            { initialProps: { input: input1 } },
        );

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(1);
        });

        rerender({ input: input2 });

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(2);
        });
    });

    it('triggers a new API call when databaseName changes', async () => {
        const input1 = makeInput({ databaseName: `db1-${testCounter}` });
        const input2 = makeInput({ databaseName: `db2-${testCounter}` });
        mockApiFetch.mockResolvedValue(makeSuccessResponse());

        const { rerender } = renderHook(
            ({ input }: { input: QueryOverviewInput }) =>
                useQueryOverview(input),
            { initialProps: { input: input1 } },
        );

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(1);
        });

        rerender({ input: input2 });

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(2);
        });
    });

    it('refresh clears cache and retriggers the API call', async () => {
        const input = makeInput();
        mockApiFetch.mockResolvedValue(makeSuccessResponse());

        const { result } = renderHook(() => useQueryOverview(input));

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(1);
        });

        // Call refresh to clear cache and regenerate
        await act(async () => {
            result.current.refresh();
        });

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(2);
        });
    });

    it('refresh is a no-op when input is null', () => {
        const { result } = renderHook(() => useQueryOverview(null));

        // Should not throw
        result.current.refresh();
        expect(mockApiFetch).not.toHaveBeenCalled();
    });

    it('includes buffer hit ratio in user message', async () => {
        const input = makeInput({
            sharedBlksHit: 900,
            sharedBlksRead: 100,
        });

        renderHook(() => useQueryOverview(input));

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(1);
        });

        const body = JSON.parse(mockApiFetch.mock.calls[0][1].body);
        expect(body.messages[0].content).toContain('90.0%');
    });

    it('truncates long query text in user message', async () => {
        const longQuery = `SELECT ${'a'.repeat(300)} FROM t`;
        const input = makeInput({ queryText: longQuery });

        renderHook(() => useQueryOverview(input));

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(1);
        });

        const body = JSON.parse(mockApiFetch.mock.calls[0][1].body);
        expect(body.messages[0].content).toContain('...');
        // The truncated text should be at most 200 characters plus ellipsis
        const queryLine = body.messages[0].content.split('\n')[0];
        expect(queryLine.length).toBeLessThan(220);
    });
});
