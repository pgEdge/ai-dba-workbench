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
