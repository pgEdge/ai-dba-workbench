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
import { renderHook, act, waitFor } from '@testing-library/react';
import { usePerformanceSummary } from '../usePerformanceSummary';
import { DEFAULT_RETRY_BASE_DELAY_MS } from '../../../../hooks/useRetryingFetch';
import type {
    ServerSelection,
    ClusterSelection,
    EstateSelection,
} from '../../../../types/selection';
import { MAX_CONNECTION_IDS_PER_REQUEST } from '../../../../utils/connectionIdBatches';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const mockApiFetch = vi.fn();

vi.mock('../../../../utils/apiClient', () => ({
    apiFetch: (...args: unknown[]) => mockApiFetch(...args),
}));

const mockUser = { id: 1, username: 'testuser' };
let mockAuthUser: typeof mockUser | null = mockUser;

vi.mock('../../../../contexts/useAuth', () => ({
    useAuth: () => ({ user: mockAuthUser }),
}));

let mockLastRefresh = 0;

vi.mock('../../../../contexts/useClusterData', () => ({
    useClusterData: () => ({ lastRefresh: mockLastRefresh }),
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function okResponse(body: unknown): Response {
    return {
        ok: true,
        status: 200,
        json: () => Promise.resolve(body),
    } as unknown as Response;
}

function errorResponse(status = 500, body: unknown = {}): Response {
    return {
        ok: false,
        status,
        json: () => Promise.resolve(body),
    } as unknown as Response;
}

const summaryBody = { time_range: '24h', connections: [] };

const serverSelection: ServerSelection = {
    type: 'server',
    id: 7,
    name: 'server-7',
    status: 'online',
    description: '',
    host: 'localhost',
    port: 5432,
    role: 'primary',
    version: '16',
    database: 'postgres',
    username: 'postgres',
    os: 'linux',
    platform: 'x86_64',
};

const clusterSelection: ClusterSelection = {
    type: 'cluster',
    id: 'cluster-1',
    name: 'Cluster 1',
    status: 'online',
    description: '',
    servers: [{ id: 1, name: 's1' }, { id: 2, name: 's2' }],
    serverIds: [1, 2],
};

const estateSelection: EstateSelection = {
    type: 'estate',
    name: 'Estate',
    status: 'online',
    groups: [
        {
            name: 'group-1',
            clusters: [
                {
                    name: 'c1',
                    servers: [{ id: 10, name: 's10' }, { id: 11, name: 's11' }],
                },
            ],
        },
    ] as unknown as EstateSelection['groups'],
};

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('usePerformanceSummary', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockAuthUser = mockUser;
        mockLastRefresh = 0;
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    it('returns null data and does not fetch for a null selection', async () => {
        const { result } = renderHook(() => usePerformanceSummary(null));

        await act(async () => {
            await Promise.resolve();
        });

        expect(mockApiFetch).not.toHaveBeenCalled();
        expect(result.current.data).toBeNull();
        expect(result.current.retrying).toBe(false);
    });

    it('does not fetch when there is no user', async () => {
        mockAuthUser = null;
        const { result } = renderHook(() =>
            usePerformanceSummary(serverSelection),
        );

        await act(async () => {
            await Promise.resolve();
        });

        expect(mockApiFetch).not.toHaveBeenCalled();
        expect(result.current.data).toBeNull();
    });

    it('fetches performance data for a server selection', async () => {
        mockApiFetch.mockResolvedValue(okResponse(summaryBody));

        const { result } = renderHook(() =>
            usePerformanceSummary(serverSelection),
        );

        await waitFor(() => {
            expect(result.current.data).toEqual(summaryBody);
        });

        expect(mockApiFetch).toHaveBeenCalledWith(
            '/api/v1/metrics/performance-summary?connection_id=7&time_range=24h',
        );
        expect(result.current.error).toBeNull();
        expect(result.current.retrying).toBe(false);
    });

    it('builds a multi-connection URL for a cluster selection', async () => {
        mockApiFetch.mockResolvedValue(okResponse(summaryBody));

        renderHook(() => usePerformanceSummary(clusterSelection));

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalled();
        });
        expect(mockApiFetch).toHaveBeenCalledWith(
            '/api/v1/metrics/performance-summary?connection_ids=1,2&time_range=24h',
        );
    });

    it('builds a multi-connection URL for an estate selection', async () => {
        mockApiFetch.mockResolvedValue(okResponse(summaryBody));

        renderHook(() => usePerformanceSummary(estateSelection));

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalled();
        });
        expect(mockApiFetch).toHaveBeenCalledWith(
            '/api/v1/metrics/performance-summary?connection_ids=10,11&time_range=24h',
        );
    });

    it('yields null data when a server selection has no id', async () => {
        const noId = { ...serverSelection, id: undefined } as unknown as ServerSelection;
        const { result } = renderHook(() => usePerformanceSummary(noId));

        await act(async () => {
            await Promise.resolve();
        });

        expect(mockApiFetch).not.toHaveBeenCalled();
        expect(result.current.data).toBeNull();
    });

    it('yields null data when a cluster selection has no server ids', async () => {
        const empty = { ...clusterSelection, serverIds: [] };
        const { result } = renderHook(() => usePerformanceSummary(empty));

        await act(async () => {
            await Promise.resolve();
        });

        expect(mockApiFetch).not.toHaveBeenCalled();
        expect(result.current.data).toBeNull();
    });

    it('surfaces the server error message on a non-ok response', async () => {
        mockApiFetch.mockResolvedValue(
            errorResponse(500, { error: 'boom from server' }),
        );

        const { result } = renderHook(() =>
            usePerformanceSummary(serverSelection),
        );

        await waitFor(() => {
            expect(result.current.error).toBe('boom from server');
        });
        expect(result.current.data).toBeNull();
    });

    it('retries after a failed fetch and heals on recovery', async () => {
        vi.useFakeTimers();
        mockApiFetch
            .mockRejectedValueOnce(new Error('network down'))
            .mockResolvedValue(okResponse(summaryBody));

        const { result } = renderHook(() =>
            usePerformanceSummary(serverSelection),
        );

        await act(async () => {
            await Promise.resolve();
            await Promise.resolve();
        });

        expect(result.current.error).toBe('network down');
        expect(result.current.retrying).toBe(true);

        await act(async () => {
            await vi.advanceTimersByTimeAsync(DEFAULT_RETRY_BASE_DELAY_MS);
        });

        expect(mockApiFetch).toHaveBeenCalledTimes(2);
        expect(result.current.data).toEqual(summaryBody);
        expect(result.current.retrying).toBe(false);
    });
});

describe('usePerformanceSummary connection_ids batching', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockAuthUser = mockUser;
        mockLastRefresh = 0;
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    const ids = (count: number): number[] =>
        Array.from({ length: count }, (_, i) => i + 1);

    const requestedIds = (url: string): number[] =>
        (new URLSearchParams(url.split('?')[1]).get('connection_ids') ?? '')
            .split(',')
            .map(Number);

    // One connection entry per requested id, plus an aggregate, so the
    // merge can be compared against the unbatched response.
    const bodyFor = (url: string) => ({
        time_range: '24h',
        connections: requestedIds(url).map(id => ({ connection_id: id })),
        aggregate: { cache_hit_ratio: 99, commits_per_sec: 1, rollback_percent: 0 },
    });

    const bigCluster = (count: number): ClusterSelection => ({
        ...clusterSelection,
        servers: ids(count).map(id => ({ id, name: `s${id}` })),
        serverIds: ids(count),
    });

    it('sends one request and returns it verbatim within the cap', async () => {
        mockApiFetch.mockImplementation((url: string) =>
            Promise.resolve(okResponse(bodyFor(url))),
        );

        const selection = bigCluster(MAX_CONNECTION_IDS_PER_REQUEST);
        const { result } = renderHook(() => usePerformanceSummary(selection));

        await waitFor(() => {
            expect(result.current.data).not.toBeNull();
        });

        expect(mockApiFetch).toHaveBeenCalledTimes(1);
        // A single response is passed through untouched, aggregate and all.
        expect(result.current.data?.aggregate).toBeDefined();
        expect(result.current.data?.connections).toHaveLength(
            MAX_CONNECTION_IDS_PER_REQUEST,
        );
    });

    it('batches an over-cap cluster and concatenates the connections', async () => {
        mockApiFetch.mockImplementation((url: string) =>
            Promise.resolve(okResponse(bodyFor(url))),
        );

        const selection = bigCluster(250);
        const { result } = renderHook(() => usePerformanceSummary(selection));

        await waitFor(() => {
            expect(result.current.data).not.toBeNull();
        });

        const urls = mockApiFetch.mock.calls.map(call => call[0] as string);
        expect(urls).toHaveLength(3);
        expect(requestedIds(urls[0])).toEqual(ids(250).slice(0, 100));
        expect(requestedIds(urls[1])).toEqual(ids(250).slice(100, 200));
        expect(requestedIds(urls[2])).toEqual(ids(250).slice(200));

        // The merged result matches what one request would have returned,
        // except that the unmergeable aggregate is dropped.
        expect(result.current.data?.time_range).toBe('24h');
        expect(
            result.current.data?.connections.map(c => c.connection_id),
        ).toEqual(ids(250));
        expect(result.current.data?.aggregate).toBeUndefined();
    });

    it('surfaces a failing batch as an error and keeps no partial data', async () => {
        mockApiFetch.mockImplementation((url: string) =>
            requestedIds(url)[0] === 101
                ? Promise.resolve(errorResponse(500, { error: 'batch failed' }))
                : Promise.resolve(okResponse(bodyFor(url))),
        );

        // The selection is hoisted so every render passes the same
        // object; a fresh one each render would restart the fetch.
        const selection = bigCluster(250);
        const { result } = renderHook(() => usePerformanceSummary(selection));

        await waitFor(() => {
            expect(result.current.error).toBe('batch failed');
        });
        expect(result.current.data).toBeNull();
    });
});
