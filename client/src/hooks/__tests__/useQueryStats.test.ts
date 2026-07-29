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
vi.mock('../../contexts/useDashboard', () => ({
    useDashboard: () => ({ refreshTrigger: mockRefreshTrigger }),
}));

vi.mock('../../utils/logger', () => ({
    logger: { error: vi.fn(), warn: vi.fn(), info: vi.fn(), debug: vi.fn() },
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const params: QueryStatsParams = {
    connectionId: 7,
    queryid: '-1234567890',
    timeRange: '6h',
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
        expect(url).toContain('time_range=6h');
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
});
