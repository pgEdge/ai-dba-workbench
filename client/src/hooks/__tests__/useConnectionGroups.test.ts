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
import {
    useConnectionGroups,
    type ConnectionGroupsParams,
} from '../useConnectionGroups';
import type {
    ConnectionGroupRow,
} from '../../components/Dashboard/ServerDashboard/types';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const mockApiGet = vi.fn();
vi.mock('../../utils/apiClient', () => ({
    apiGet: (...args: unknown[]) => mockApiGet(...args),
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

const params: ConnectionGroupsParams = {
    connectionId: 7,
    groupBy: 'user',
    timeRange: '6h',
};

/** Build a group row with sensible defaults. */
const makeGroupRow = (
    overrides: Partial<ConnectionGroupRow> = {},
): ConnectionGroupRow => ({
    group_label: 'app_rw',
    client_hostname: null,
    total: 16,
    active: 12,
    idle: 4,
    idle_in_transaction: 0,
    other: 0,
    ...overrides,
});

/** Build a successful response payload. */
const makeResponse = (
    groups: ConnectionGroupRow[] = [makeGroupRow()],
    collectedAt: string | null = '2026-07-29T10:00:00Z',
) => ({ collected_at: collectedAt, groups });

/** Extract a query parameter from the most recent request URL. */
const paramOf = (callIndex: number, name: string): string | null => {
    const url = mockApiGet.mock.calls[callIndex][0] as string;
    return new URL(url, 'https://example.test').searchParams.get(name);
};

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('useConnectionGroups', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockUser = { id: 1, username: 'testuser' };
        mockRefreshTrigger = 0;
    });

    afterEach(() => {
        vi.resetAllMocks();
    });

    it('returns idle state and issues no request when params are null', () => {
        const { result } = renderHook(() => useConnectionGroups(null));

        expect(result.current.groups).toEqual([]);
        expect(result.current.collectedAt).toBeNull();
        expect(result.current.loading).toBe(false);
        expect(result.current.error).toBeNull();
        expect(mockApiGet).not.toHaveBeenCalled();
    });

    it('issues no request without an authenticated user', () => {
        mockUser = null;

        renderHook(() => useConnectionGroups(params));

        expect(mockApiGet).not.toHaveBeenCalled();
    });

    it('fetches groups for the given connection, grouping, and period',
        async () => {
            mockApiGet.mockResolvedValue(makeResponse());

            const { result } = renderHook(
                () => useConnectionGroups(params),
            );

            await waitFor(() => {
                expect(result.current.groups).toHaveLength(1);
            });

            const url = mockApiGet.mock.calls[0][0] as string;
            expect(url).toContain('/api/v1/metrics/connection-groups?');
            expect(paramOf(0, 'connection_id')).toBe('7');
            expect(paramOf(0, 'group_by')).toBe('user');
            expect(paramOf(0, 'time_range')).toBe('6h');
            expect(result.current.groups[0].group_label).toBe('app_rw');
            expect(result.current.collectedAt)
                .toBe('2026-07-29T10:00:00Z');
            expect(result.current.loading).toBe(false);
            expect(result.current.error).toBeNull();
        });

    it('handles an empty grouping list with a null timestamp',
        async () => {
            mockApiGet.mockResolvedValue(makeResponse([], null));

            const { result } = renderHook(
                () => useConnectionGroups(params),
            );

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });
            expect(result.current.groups).toEqual([]);
            expect(result.current.collectedAt).toBeNull();
            expect(result.current.error).toBeNull();
        });

    it('tolerates a malformed groups field', async () => {
        mockApiGet.mockResolvedValue({
            collected_at: null,
            groups: null,
        });

        const { result } = renderHook(() => useConnectionGroups(params));

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });
        expect(result.current.groups).toEqual([]);
    });

    it('surfaces the error message from a failed request', async () => {
        mockApiGet.mockRejectedValue(
            new Error('connection groups unavailable'),
        );

        const { result } = renderHook(() => useConnectionGroups(params));

        await waitFor(() => {
            expect(result.current.error)
                .toBe('connection groups unavailable');
        });
        expect(result.current.groups).toEqual([]);
        expect(result.current.collectedAt).toBeNull();
    });

    it('falls back to a generic message when the error has none',
        async () => {
            mockApiGet.mockRejectedValue(new Error(''));

            const { result } = renderHook(
                () => useConnectionGroups(params),
            );

            await waitFor(() => {
                expect(result.current.error)
                    .toBe('Failed to fetch connection groups');
            });
        });

    it('refetches when the grouping changes', async () => {
        mockApiGet.mockResolvedValue(makeResponse());

        const { rerender } = renderHook(
            ({ p }: { p: ConnectionGroupsParams }) => useConnectionGroups(p),
            { initialProps: { p: params } },
        );

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledTimes(1);
        });

        rerender({ p: { ...params, groupBy: 'client' } });

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledTimes(2);
        });
        expect(paramOf(1, 'group_by')).toBe('client');
    });

    it('clears previous rows when the grouping changes', async () => {
        mockApiGet.mockResolvedValueOnce(makeResponse());
        mockApiGet.mockReturnValueOnce(new Promise(() => {}));

        const { result, rerender } = renderHook(
            ({ p }: { p: ConnectionGroupsParams }) => useConnectionGroups(p),
            { initialProps: { p: params } },
        );

        await waitFor(() => {
            expect(result.current.groups).toHaveLength(1);
        });

        rerender({ p: { ...params, groupBy: 'database' } });

        await waitFor(() => {
            expect(result.current.groups).toEqual([]);
        });
        expect(result.current.loading).toBe(true);
        expect(result.current.collectedAt).toBeNull();
    });

    it('refetches when the time range changes', async () => {
        mockApiGet.mockResolvedValue(makeResponse());

        const { rerender } = renderHook(
            ({ p }: { p: ConnectionGroupsParams }) => useConnectionGroups(p),
            { initialProps: { p: params } },
        );

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledTimes(1);
        });

        rerender({ p: { ...params, timeRange: '24h' } });

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledTimes(2);
        });
        expect(paramOf(1, 'time_range')).toBe('24h');
    });

    it('refetches when the connection changes', async () => {
        mockApiGet.mockResolvedValue(makeResponse());

        const { rerender } = renderHook(
            ({ p }: { p: ConnectionGroupsParams }) => useConnectionGroups(p),
            { initialProps: { p: params } },
        );

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledTimes(1);
        });

        rerender({ p: { ...params, connectionId: 9 } });

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledTimes(2);
        });
        expect(paramOf(1, 'connection_id')).toBe('9');
    });

    it('refetches when the refresh trigger changes', async () => {
        mockApiGet.mockResolvedValue(makeResponse());

        const { rerender } = renderHook(() => useConnectionGroups(params));

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledTimes(1);
        });

        mockRefreshTrigger = 1;
        rerender();

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledTimes(2);
        });
    });

    it('does not show the spinner on an auto-refresh', async () => {
        mockApiGet.mockResolvedValueOnce(makeResponse());
        mockApiGet.mockReturnValueOnce(new Promise(() => {}));

        const { result, rerender } = renderHook(
            () => useConnectionGroups(params),
        );

        await waitFor(() => {
            expect(result.current.groups).toHaveLength(1);
        });

        mockRefreshTrigger = 1;
        rerender();

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledTimes(2);
        });
        expect(result.current.loading).toBe(false);
        expect(result.current.groups).toHaveLength(1);
    });

    it('refetches on demand', async () => {
        mockApiGet.mockResolvedValue(makeResponse());

        const { result } = renderHook(() => useConnectionGroups(params));

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledTimes(1);
        });

        await act(async () => {
            result.current.refetch();
        });

        expect(mockApiGet).toHaveBeenCalledTimes(2);
    });

    it('ignores a response that lands after unmount', async () => {
        let resolveFetch: (value: unknown) => void = () => {};
        mockApiGet.mockReturnValue(
            new Promise(resolve => { resolveFetch = resolve; }),
        );

        const { result, unmount } = renderHook(
            () => useConnectionGroups(params),
        );

        unmount();

        await act(async () => {
            resolveFetch(makeResponse());
        });

        expect(result.current.groups).toEqual([]);
    });
});
