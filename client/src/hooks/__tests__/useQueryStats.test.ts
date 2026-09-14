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
import { renderHook, waitFor, act } from '@testing-library/react';
import { useQueryStats, type QueryStatsParams } from '../useQueryStats';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const mockApiFetch = vi.fn();
vi.mock('../../utils/apiClient', () => ({
    apiFetch: (...args: unknown[]) => mockApiFetch(...args),
}));

let mockUser: { id: number; username: string } | null = {
    id: 1,
    username: 'testuser',
};
vi.mock('../../contexts/useAuth', () => ({
    useAuth: () => ({ user: mockUser }),
}));

let mockRefreshTrigger = 0;
let mockTimeRange: {
    range: string;
    customStart?: string;
    customEnd?: string;
} = { range: '6h' };
vi.mock('../../contexts/useDashboard', () => ({
    useDashboard: () => ({
        refreshTrigger: mockRefreshTrigger,
        timeRange: mockTimeRange,
    }),
}));

vi.mock('../../utils/logger', () => ({
    logger: { error: vi.fn(), warn: vi.fn(), info: vi.fn(), debug: vi.fn() },
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const params: QueryStatsParams = {
    connectionId: 7,
    queryId: '-1234567890',
    databaseName: 'testdb',
    timeRange: '6h',
};

const CUSTOM_START = '2026-05-01T00:00:00.000Z';
const CUSTOM_END = '2026-05-02T00:00:00.000Z';

/** A fetch that stays pending until the returned resolver is called. */
const deferredFetch = (): ((value: unknown) => void) => {
    let resolveFetch: (value: unknown) => void = () => {};
    mockApiFetch.mockReturnValueOnce(
        new Promise(resolve => { resolveFetch = resolve; }),
    );
    return (value: unknown) => { resolveFetch(value); };
};

const makeStats = (avg: number | null = 42.5) => ({
    queryid: '-1234567890',
    avg_exec_time: avg,
    calls: 12,
    total_exec_time: 510,
});

const okResponse = (data: unknown) => ({
    ok: true,
    json: () => Promise.resolve(data),
});

const errorResponse = (
    status: number,
    body: Record<string, string> | null = {},
) => ({
    ok: false,
    status,
    json: () => body === null
        ? Promise.reject(new Error('no body'))
        : Promise.resolve(body),
});

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('useQueryStats', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockUser = { id: 1, username: 'testuser' };
        mockRefreshTrigger = 0;
        mockTimeRange = { range: '6h' };
    });

    afterEach(() => {
        vi.resetAllMocks();
    });

    it('returns idle state and issues no request when params are null', () => {
        const { result } = renderHook(() => useQueryStats(null));

        expect(result.current.stats).toBeNull();
        expect(result.current.loading).toBe(false);
        expect(result.current.error).toBeNull();
        expect(mockApiFetch).not.toHaveBeenCalled();
    });

    it('issues no request without an authenticated user', () => {
        mockUser = null;

        renderHook(() => useQueryStats(params));

        expect(mockApiFetch).not.toHaveBeenCalled();
    });

    it('fetches stats for the given query and period', async () => {
        mockApiFetch.mockResolvedValue(okResponse(makeStats()));

        const { result } = renderHook(() => useQueryStats(params));

        await waitFor(() => {
            expect(result.current.stats).not.toBeNull();
        });

        const url = mockApiFetch.mock.calls[0][0] as string;
        expect(url).toContain('/api/v1/metrics/query-stats?');
        expect(url).toContain('connection_id=7');
        expect(url).toContain('queryid=-1234567890');
        expect(url).toContain('database_name=testdb');
        expect(url).toContain('time_range=6h');
        expect(url).not.toContain('time_start=');
        expect(url).not.toContain('time_end=');
        expect(result.current.stats?.avg_exec_time).toBe(42.5);
        expect(result.current.loading).toBe(false);
        expect(result.current.error).toBeNull();
    });

    it('preserves a null average rather than coercing it to zero', async () => {
        mockApiFetch.mockResolvedValue(okResponse(makeStats(null)));

        const { result } = renderHook(() => useQueryStats(params));

        await waitFor(() => {
            expect(result.current.stats).not.toBeNull();
        });
        expect(result.current.stats?.avg_exec_time).toBeNull();
    });

    it('surfaces the server error message on a failed response', async () => {
        mockApiFetch.mockResolvedValue(
            errorResponse(500, { error: 'stats unavailable' }),
        );

        const { result } = renderHook(() => useQueryStats(params));

        await waitFor(() => {
            expect(result.current.error).toBe('stats unavailable');
        });
        expect(result.current.stats).toBeNull();
    });

    it('falls back to a status message when the body has no error', async () => {
        mockApiFetch.mockResolvedValue(errorResponse(503));

        const { result } = renderHook(() => useQueryStats(params));

        await waitFor(() => {
            expect(result.current.error).toContain(
                'Failed to fetch query stats: 503',
            );
        });
    });

    it('falls back to a status message when the body is unreadable', async () => {
        mockApiFetch.mockResolvedValue(errorResponse(500, null));

        const { result } = renderHook(() => useQueryStats(params));

        await waitFor(() => {
            expect(result.current.error).toContain(
                'Failed to fetch query stats: 500',
            );
        });
    });

    it('reports a network failure', async () => {
        mockApiFetch.mockRejectedValue(new Error('network down'));

        const { result } = renderHook(() => useQueryStats(params));

        await waitFor(() => {
            expect(result.current.error).toBe('network down');
        });
        expect(result.current.stats).toBeNull();
    });

    it('refetches when the time range changes', async () => {
        mockApiFetch.mockResolvedValue(okResponse(makeStats()));

        const { rerender } = renderHook(
            ({ p }: { p: QueryStatsParams }) => useQueryStats(p),
            { initialProps: { p: params } },
        );

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(1);
        });

        rerender({ p: { ...params, timeRange: '24h' } });

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(2);
        });
        const url = mockApiFetch.mock.calls[1][0] as string;
        expect(url).toContain('time_range=24h');
    });

    it('refetches when the refresh trigger changes', async () => {
        mockApiFetch.mockResolvedValue(okResponse(makeStats()));

        const { rerender } = renderHook(() => useQueryStats(params));

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(1);
        });

        mockRefreshTrigger = 1;
        rerender();

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(2);
        });
    });

    it('refetches on demand', async () => {
        mockApiFetch.mockResolvedValue(okResponse(makeStats()));

        const { result } = renderHook(() => useQueryStats(params));

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(1);
        });

        await act(async () => {
            result.current.refetch();
        });

        expect(mockApiFetch).toHaveBeenCalledTimes(2);
    });

    it('ignores a response that lands after unmount', async () => {
        let resolveFetch: (value: unknown) => void = () => {};
        mockApiFetch.mockReturnValue(
            new Promise(resolve => { resolveFetch = resolve; }),
        );

        const { result, unmount } = renderHook(
            () => useQueryStats(params),
        );

        unmount();

        await act(async () => {
            resolveFetch(okResponse(makeStats()));
        });

        expect(result.current.stats).toBeNull();
    });
    it('sends the custom bounds for a custom time range', async () => {
        mockTimeRange = {
            range: 'custom',
            customStart: CUSTOM_START,
            customEnd: CUSTOM_END,
        };
        mockApiFetch.mockResolvedValue(okResponse(makeStats()));

        const { result } = renderHook(
            () => useQueryStats({ ...params, timeRange: 'custom' }),
        );

        await waitFor(() => {
            expect(result.current.stats).not.toBeNull();
        });
        const url = mockApiFetch.mock.calls[0][0] as string;
        expect(url).toContain('time_range=custom');
        expect(url).toContain(
            `time_start=${encodeURIComponent(CUSTOM_START)}`,
        );
        expect(url).toContain(
            `time_end=${encodeURIComponent(CUSTOM_END)}`,
        );
    });

    it('issues no request for a custom range missing a bound', () => {
        mockTimeRange = { range: 'custom', customStart: CUSTOM_START };

        const { result } = renderHook(
            () => useQueryStats({ ...params, timeRange: 'custom' }),
        );

        expect(mockApiFetch).not.toHaveBeenCalled();
        expect(result.current.loading).toBe(false);
        expect(result.current.error).toBeNull();
    });

    it('refetches when the custom bounds change', async () => {
        mockTimeRange = {
            range: 'custom',
            customStart: CUSTOM_START,
            customEnd: CUSTOM_END,
        };
        mockApiFetch.mockResolvedValue(okResponse(makeStats()));

        const { rerender } = renderHook(
            () => useQueryStats({ ...params, timeRange: 'custom' }),
        );

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(1);
        });

        const laterEnd = '2026-05-03T00:00:00.000Z';
        mockTimeRange = {
            range: 'custom',
            customStart: CUSTOM_START,
            customEnd: laterEnd,
        };
        rerender();

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(2);
        });
        const url = mockApiFetch.mock.calls[1][0] as string;
        expect(url).toContain(`time_end=${encodeURIComponent(laterEnd)}`);
    });

    it('keeps the newer result when an older request completes last', async () => {
        const resolveFirst = deferredFetch();
        const resolveSecond = deferredFetch();

        const { result, rerender } = renderHook(
            ({ p }: { p: QueryStatsParams }) => useQueryStats(p),
            { initialProps: { p: params } },
        );
        rerender({ p: { ...params, timeRange: '24h' } });

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(2);
        });

        await act(async () => {
            resolveSecond(okResponse(makeStats(99)));
        });
        await waitFor(() => {
            expect(result.current.stats?.avg_exec_time).toBe(99);
        });
        expect(result.current.loading).toBe(false);

        await act(async () => {
            resolveFirst(okResponse(makeStats(1)));
        });

        expect(result.current.stats?.avg_exec_time).toBe(99);
        expect(result.current.error).toBeNull();
    });

    it('ignores an error from a superseded request', async () => {
        const resolveFirst = deferredFetch();
        const resolveSecond = deferredFetch();

        const { result, rerender } = renderHook(
            ({ p }: { p: QueryStatsParams }) => useQueryStats(p),
            { initialProps: { p: params } },
        );
        rerender({ p: { ...params, timeRange: '24h' } });

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(2);
        });

        await act(async () => {
            resolveSecond(okResponse(makeStats(99)));
        });
        await waitFor(() => {
            expect(result.current.stats?.avg_exec_time).toBe(99);
        });

        await act(async () => {
            resolveFirst(errorResponse(500, { error: 'too late' }));
        });

        expect(result.current.error).toBeNull();
        expect(result.current.stats?.avg_exec_time).toBe(99);
    });

    it('clears state and ignores a late response when params become null', async () => {
        const resolveFirst = deferredFetch();

        const { result, rerender } = renderHook(
            ({ p }: { p: QueryStatsParams | null }) => useQueryStats(p),
            { initialProps: { p: params as QueryStatsParams | null } },
        );

        await waitFor(() => {
            expect(result.current.loading).toBe(true);
        });

        rerender({ p: null });

        expect(result.current.loading).toBe(false);
        expect(result.current.stats).toBeNull();
        expect(result.current.error).toBeNull();

        await act(async () => {
            resolveFirst(okResponse(makeStats()));
        });

        expect(result.current.stats).toBeNull();
        expect(result.current.loading).toBe(false);
        expect(mockApiFetch).toHaveBeenCalledTimes(1);
    });

    it('clears previously loaded stats when params become null', async () => {
        mockApiFetch.mockResolvedValue(okResponse(makeStats()));

        const { result, rerender } = renderHook(
            ({ p }: { p: QueryStatsParams | null }) => useQueryStats(p),
            { initialProps: { p: params as QueryStatsParams | null } },
        );

        await waitFor(() => {
            expect(result.current.stats).not.toBeNull();
        });

        rerender({ p: null });

        expect(result.current.stats).toBeNull();
        expect(result.current.error).toBeNull();
        expect(result.current.loading).toBe(false);
    });
});
